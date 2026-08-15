use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};

use serde::Serialize;

pub(crate) const UPDATE_STATUS_EVENT: &str = "agentmetry://update-status";

#[derive(Clone, Default)]
pub(crate) struct UpdateCoordinator {
    in_progress: Arc<AtomicBool>,
}

impl UpdateCoordinator {
    pub(crate) fn begin(&self) -> Result<UpdateOperation, String> {
        self.in_progress
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .map_err(|_| "another update operation is already in progress".to_string())?;
        Ok(UpdateOperation {
            in_progress: Arc::clone(&self.in_progress),
        })
    }
}

pub(crate) struct UpdateOperation {
    in_progress: Arc<AtomicBool>,
}

impl Drop for UpdateOperation {
    fn drop(&mut self) {
        self.in_progress.store(false, Ordering::Release);
    }
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct UpdateCheckResponse {
    available: bool,
    current_version: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    version: Option<String>,
}

impl UpdateCheckResponse {
    pub(crate) fn current(current_version: impl Into<String>) -> Self {
        Self {
            available: false,
            current_version: current_version.into(),
            version: None,
        }
    }

    pub(crate) fn available(
        current_version: impl Into<String>,
        version: impl Into<String>,
    ) -> Self {
        Self {
            available: true,
            current_version: current_version.into(),
            version: Some(version.into()),
        }
    }
}

#[derive(Clone, Copy, Debug, Serialize)]
#[serde(rename_all = "kebab-case")]
pub(crate) enum UpdatePhase {
    Checking,
    UpToDate,
    Available,
    Downloading,
    Installing,
    Restarting,
    Failed,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct UpdateStatusEvent {
    phase: UpdatePhase,
    #[serde(skip_serializing_if = "Option::is_none")]
    current_version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    downloaded: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    total: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    message: Option<String>,
}

impl UpdateStatusEvent {
    pub(crate) fn checking(current_version: impl Into<String>) -> Self {
        Self::versioned(UpdatePhase::Checking, Some(current_version.into()), None)
    }

    pub(crate) fn up_to_date(current_version: impl Into<String>) -> Self {
        Self::versioned(UpdatePhase::UpToDate, Some(current_version.into()), None)
    }

    pub(crate) fn available(
        current_version: impl Into<String>,
        version: impl Into<String>,
    ) -> Self {
        Self::versioned(
            UpdatePhase::Available,
            Some(current_version.into()),
            Some(version.into()),
        )
    }

    pub(crate) fn phase(phase: UpdatePhase, version: impl Into<String>) -> Self {
        Self::versioned(phase, None, Some(version.into()))
    }

    pub(crate) fn progress(
        phase: UpdatePhase,
        version: impl Into<String>,
        downloaded: u64,
        total: Option<u64>,
    ) -> Self {
        Self {
            phase,
            current_version: None,
            version: Some(version.into()),
            downloaded: Some(downloaded),
            total,
            message: None,
        }
    }

    pub(crate) fn failed(message: impl Into<String>) -> Self {
        let mut event = Self::versioned(UpdatePhase::Failed, None, None);
        event.message = Some(message.into());
        event
    }

    fn versioned(
        phase: UpdatePhase,
        current_version: Option<String>,
        version: Option<String>,
    ) -> Self {
        Self {
            phase,
            current_version,
            version,
            downloaded: None,
            total: None,
            message: None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{UpdateCheckResponse, UpdateCoordinator, UpdatePhase, UpdateStatusEvent};

    #[test]
    fn update_operations_are_exclusive_and_release_on_drop() {
        let coordinator = UpdateCoordinator::default();
        let operation = coordinator.begin().expect("first update operation");

        assert!(coordinator.begin().is_err());
        drop(operation);
        assert!(coordinator.begin().is_ok());
    }

    #[test]
    fn update_contracts_serialize_for_the_frontend() {
        assert_eq!(
            serde_json::to_value(UpdateCheckResponse::available("1.0.2", "1.1.0")).unwrap(),
            serde_json::json!({
                "available": true,
                "currentVersion": "1.0.2",
                "version": "1.1.0"
            })
        );
        assert_eq!(
            serde_json::to_value(UpdateStatusEvent::progress(
                UpdatePhase::Downloading,
                "1.1.0",
                25,
                Some(100),
            ))
            .unwrap(),
            serde_json::json!({
                "phase": "downloading",
                "version": "1.1.0",
                "downloaded": 25,
                "total": 100
            })
        );
    }
}
