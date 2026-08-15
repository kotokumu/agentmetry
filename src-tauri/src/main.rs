use std::{
    io::{Read, Write},
    net::TcpStream,
    path::PathBuf,
    sync::Mutex,
    thread,
    time::Duration,
};

use tauri::{
    menu::MenuBuilder,
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    webview::WebviewWindowBuilder,
    Emitter, Manager, RunEvent, State, WebviewUrl, WebviewWindow, WindowEvent,
};
use tauri_plugin_shell::{
    process::{CommandChild, CommandEvent},
    ShellExt,
};
use tauri_plugin_updater::UpdaterExt;

mod updates;

use updates::{
    UpdateCheckResponse, UpdateCoordinator, UpdatePhase, UpdateStatusEvent, UPDATE_STATUS_EVENT,
};

const DASHBOARD_ADDRESS: &str = "127.0.0.1:17890";
const OTLP_HTTP_ADDRESS: &str = "127.0.0.1:4318";
const OTLP_GRPC_ADDRESS: &str = "127.0.0.1:4317";
const STARTUP_TIMEOUT: Duration = Duration::from_secs(10);

struct SidecarController {
    child: Option<CommandChild>,
    database: PathBuf,
}

impl SidecarController {
    fn new(database: PathBuf) -> Self {
        Self {
            child: None,
            database,
        }
    }

    fn is_running(&self) -> bool {
        self.child.is_some()
    }

    fn launch(&mut self, app: &tauri::AppHandle) -> Result<(), String> {
        if self.is_running() {
            return Ok(());
        }

        let database = self.database.to_string_lossy().to_string();
        let sidecar = app
            .shell()
            .sidecar("agentmetry")
            .map_err(|error| format!("resolve Agentmetry sidecar: {error}"))?
            .args([
                "--http-address",
                DASHBOARD_ADDRESS,
                "--otlp-http-address",
                OTLP_HTTP_ADDRESS,
                "--otlp-grpc-address",
                OTLP_GRPC_ADDRESS,
                "--database",
                database.as_str(),
            ]);
        let (mut events, child) = sidecar
            .spawn()
            .map_err(|error| format!("start Agentmetry sidecar: {error}"))?;
        self.child = Some(child);

        tauri::async_runtime::spawn(async move {
            while let Some(event) = events.recv().await {
                match event {
                    CommandEvent::Stdout(line) => {
                        eprintln!("agentmetryd: {}", String::from_utf8_lossy(&line));
                    }
                    CommandEvent::Stderr(line) => {
                        eprintln!("agentmetryd: {}", String::from_utf8_lossy(&line));
                    }
                    CommandEvent::Error(error) => {
                        eprintln!("agentmetryd process error: {error}");
                    }
                    CommandEvent::Terminated(payload) => {
                        eprintln!("agentmetryd terminated: {payload:?}");
                    }
                    _ => {}
                }
            }
        });

        if let Err(error) = wait_for_sidecar(DASHBOARD_ADDRESS, STARTUP_TIMEOUT) {
            self.shutdown();
            return Err(format!("wait for Agentmetry sidecar: {error}"));
        }
        Ok(())
    }

    fn shutdown(&mut self) {
        if let Some(child) = self.child.take() {
            let _ = child.kill();
        }
    }
}

struct SidecarState(Mutex<SidecarController>);

fn main() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(UpdateCoordinator::default())
        .invoke_handler(tauri::generate_handler![
            check_for_app_update,
            install_app_update
        ])
        .setup(|app| {
            let data_dir = app
                .path()
                .app_data_dir()
                .map_err(|error| format!("resolve Agentmetry data directory: {error}"))?;
            std::fs::create_dir_all(&data_dir)
                .map_err(|error| format!("create Agentmetry data directory: {error}"))?;

            let database = data_dir.join("agentmetry.db");
            let mut controller = SidecarController::new(database);
            controller.launch(app.handle())?;
            app.manage(SidecarState(Mutex::new(controller)));

            let window = build_main_window(app)?;
            build_tray(app)?;
            window
                .show()
                .map_err(|error| format!("show Agentmetry window: {error}"))?;
            if !cfg!(debug_assertions) {
                spawn_update_check(app.handle().clone());
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if window.label() == "main" {
                if let WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .on_menu_event(|app, event| match event.id().as_ref() {
            "open" => show_main_window(app),
            "hide" => hide_main_window(app),
            "quit" => {
                let _ = with_controller(app, |controller, _| {
                    controller.shutdown();
                    Ok(())
                });
                app.exit(0);
            }
            _ => {}
        })
        .build(tauri::generate_context!())
        .expect("error while building Agentmetry desktop application");

    app.run(|app, event| match event {
        #[cfg(target_os = "macos")]
        RunEvent::Reopen {
            has_visible_windows,
            ..
        } => restore_main_window_on_reopen(app, has_visible_windows),
        RunEvent::Exit => {
            let _ = with_controller(app, |controller, _| {
                controller.shutdown();
                Ok(())
            });
        }
        _ => {}
    });
}

fn spawn_update_check(app: tauri::AppHandle) {
    tauri::async_runtime::spawn(async move {
        if let Err(error) = install_available_update(app).await {
            eprintln!("Agentmetry update check failed: {error}");
        }
    });
}

#[tauri::command]
async fn check_for_app_update(
    app: tauri::AppHandle,
    coordinator: State<'_, UpdateCoordinator>,
) -> Result<UpdateCheckResponse, String> {
    let coordinator = coordinator.inner().clone();
    let _operation = coordinator.begin()?;
    check_available_update(&app).await
}

#[tauri::command]
async fn install_app_update(
    app: tauri::AppHandle,
    coordinator: State<'_, UpdateCoordinator>,
) -> Result<UpdateCheckResponse, String> {
    let coordinator = coordinator.inner().clone();
    let _operation = coordinator.begin()?;
    install_available_update_inner(app).await
}

async fn install_available_update(app: tauri::AppHandle) -> Result<UpdateCheckResponse, String> {
    let coordinator = app.state::<UpdateCoordinator>().inner().clone();
    let _operation = coordinator.begin()?;
    install_available_update_inner(app).await
}

async fn check_available_update(app: &tauri::AppHandle) -> Result<UpdateCheckResponse, String> {
    let current_version = app.package_info().version.to_string();
    emit_update_status(app, UpdateStatusEvent::checking(&current_version));
    match query_available_update(app).await {
        Ok(Some(update)) => {
            emit_update_status(
                app,
                UpdateStatusEvent::available(&update.current_version, &update.version),
            );
            Ok(UpdateCheckResponse::available(
                update.current_version,
                update.version,
            ))
        }
        Ok(None) => {
            emit_update_status(app, UpdateStatusEvent::up_to_date(&current_version));
            Ok(UpdateCheckResponse::current(current_version))
        }
        Err(error) => {
            emit_update_status(app, UpdateStatusEvent::failed(&error));
            Err(error)
        }
    }
}

async fn install_available_update_inner(
    app: tauri::AppHandle,
) -> Result<UpdateCheckResponse, String> {
    let current_version = app.package_info().version.to_string();
    emit_update_status(&app, UpdateStatusEvent::checking(&current_version));
    let update = match query_available_update(&app).await {
        Ok(Some(update)) => update,
        Ok(None) => {
            emit_update_status(&app, UpdateStatusEvent::up_to_date(&current_version));
            return Ok(UpdateCheckResponse::current(current_version));
        }
        Err(error) => {
            emit_update_status(&app, UpdateStatusEvent::failed(&error));
            return Err(error);
        }
    };

    let next_version = update.version.clone();
    emit_update_status(
        &app,
        UpdateStatusEvent::available(&update.current_version, &next_version),
    );
    let progress_app = app.clone();
    let progress_version = next_version.clone();
    let mut downloaded = 0_u64;
    let package = match update
        .download(
            move |chunk_length, content_length| {
                downloaded += chunk_length as u64;
                emit_update_status(
                    &progress_app,
                    UpdateStatusEvent::progress(
                        UpdatePhase::Downloading,
                        &progress_version,
                        downloaded,
                        content_length,
                    ),
                );
            },
            || {},
        )
        .await
    {
        Ok(package) => package,
        Err(error) => {
            let message = format!("download update {next_version}: {error}");
            emit_update_status(&app, UpdateStatusEvent::failed(&message));
            return Err(message);
        }
    };

    emit_update_status(
        &app,
        UpdateStatusEvent::phase(UpdatePhase::Installing, &next_version),
    );
    if let Err(error) = with_controller(&app, |controller, _| {
        controller.shutdown();
        Ok(())
    }) {
        emit_update_status(&app, UpdateStatusEvent::failed(&error));
        return Err(error);
    }
    if let Err(error) = update.install(package) {
        if let Err(relaunch_error) =
            with_controller(&app, |controller, handle| controller.launch(handle))
        {
            eprintln!("restart Agentmetry sidecar after update failure: {relaunch_error}");
        }
        let message = format!("install update {next_version}: {error}");
        emit_update_status(&app, UpdateStatusEvent::failed(&message));
        return Err(message);
    }

    emit_update_status(
        &app,
        UpdateStatusEvent::phase(UpdatePhase::Restarting, &next_version),
    );
    app.restart();
}

async fn query_available_update(
    app: &tauri::AppHandle,
) -> Result<Option<tauri_plugin_updater::Update>, String> {
    let updater = app
        .updater()
        .map_err(|error| format!("initialize updater: {error}"))?;
    let update = updater
        .check()
        .await
        .map_err(|error| format!("check for update: {error}"))?;
    Ok(update)
}

fn emit_update_status(app: &tauri::AppHandle, status: UpdateStatusEvent) {
    if let Err(error) = app.emit(UPDATE_STATUS_EVENT, status) {
        eprintln!("emit Agentmetry update status: {error}");
    }
}

fn build_main_window(app: &tauri::App) -> Result<WebviewWindow, Box<dyn std::error::Error>> {
    let url = url::Url::parse("http://127.0.0.1:17890/")?;
    let window = WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))
        .title("Agentmetry")
        .inner_size(1440.0, 960.0)
        .min_inner_size(960.0, 640.0)
        .build()?;
    Ok(window)
}

fn build_tray(app: &tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let menu = MenuBuilder::new(app)
        .text("open", "Open Agentmetry")
        .text("hide", "Hide Window")
        .separator()
        .text("quit", "Quit Agentmetry")
        .build()?;
    let mut tray = TrayIconBuilder::new()
        .menu(&menu)
        .tooltip("Agentmetry")
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main_window(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon().cloned() {
        tray = tray.icon(icon);
    }
    tray.build(app)?;
    Ok(())
}

fn with_controller<T>(
    app: &tauri::AppHandle,
    operation: impl FnOnce(&mut SidecarController, &tauri::AppHandle) -> Result<T, String>,
) -> Result<T, String> {
    let state = app
        .try_state::<SidecarState>()
        .ok_or_else(|| "Agentmetry sidecar state is unavailable".to_string())?;
    let mut controller = state
        .0
        .lock()
        .map_err(|_| "Agentmetry sidecar state is poisoned".to_string())?;
    operation(&mut controller, app)
}

fn restore_main_window_on_reopen(app: &tauri::AppHandle, has_visible_windows: bool) {
    if should_restore_main_window_on_reopen(has_visible_windows) {
        show_main_window(app);
    }
}

fn should_restore_main_window_on_reopen(has_visible_windows: bool) -> bool {
    !has_visible_windows
}

fn show_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn hide_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.hide();
    }
}

fn wait_for_sidecar(address: &str, timeout: Duration) -> Result<(), String> {
    let started = std::time::Instant::now();
    while started.elapsed() < timeout {
        if health_check(address) {
            return Ok(());
        }
        thread::sleep(Duration::from_millis(100));
    }
    Err(format!(
        "{address}/healthz did not become ready within {timeout:?}"
    ))
}

fn health_check(address: &str) -> bool {
    let Ok(mut stream) = TcpStream::connect(address) else {
        return false;
    };
    let _ = stream.set_read_timeout(Some(Duration::from_millis(500)));
    let _ = stream.set_write_timeout(Some(Duration::from_millis(500)));
    if stream
        .write_all(b"GET /healthz HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
        .is_err()
    {
        return false;
    }

    let mut response = Vec::new();
    if stream.read_to_end(&mut response).is_err() {
        return false;
    }
    response.starts_with(b"HTTP/1.1 200 ") || response.starts_with(b"HTTP/1.0 200 ")
}

#[cfg(test)]
mod tests {
    use super::should_restore_main_window_on_reopen;

    #[test]
    fn reopen_restores_main_window_when_macos_reports_no_visible_windows() {
        assert!(should_restore_main_window_on_reopen(false));
    }

    #[test]
    fn reopen_does_not_restore_main_window_when_macos_reports_a_visible_window() {
        assert!(!should_restore_main_window_on_reopen(true));
    }
}
