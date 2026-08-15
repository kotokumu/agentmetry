fn main() {
    let attributes = tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&["check_for_app_update", "install_app_update"]),
    );

    tauri_build::try_build(attributes).expect("failed to run Tauri build script");
}
