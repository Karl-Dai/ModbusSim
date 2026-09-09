mod analytics;
mod commands;
mod connection_settings;
mod state;
pub mod update;

use state::AppState;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // tauri-plugin-aptabase starts a background task with tokio::spawn during
    // plugin initialization, so enter a Tokio runtime before building Tauri.
    let rt = tokio::runtime::Runtime::new().expect("failed to create Tokio runtime");
    let _guard = rt.enter();

    tauri::Builder::default()
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_store::Builder::new().build())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_aptabase::Builder::new(analytics::APTABASE_KEY).build())
        .manage(AppState::new())
        .manage(update::UpdateState::default())
        .invoke_handler(tauri::generate_handler![
            // Connection commands
            commands::create_master_connection,
            connection_settings::get_master_connection_settings,
            connection_settings::update_master_connection,
            commands::connect_master,
            commands::disconnect_master,
            commands::delete_master_connection,
            commands::list_master_connections,
            // Scan group commands
            commands::add_scan_group,
            commands::update_scan_group,
            commands::remove_scan_group,
            commands::list_scan_groups,
            // Polling commands
            commands::start_polling,
            commands::stop_polling,
            commands::start_all_polling,
            commands::stop_all_polling,
            commands::get_poll_data,
            // Read/Write commands
            commands::read_once,
            commands::write_single_register,
            commands::write_single_coil,
            commands::write_multiple_registers,
            commands::write_multiple_coils,
            // Log commands
            commands::get_communication_logs,
            commands::clear_communication_logs,
            commands::export_logs_csv,
            // Scan commands
            commands::start_slave_id_scan,
            commands::start_register_scan,
            commands::cancel_scan,
            // Tool commands
            commands::convert_plc_to_modbus,
            commands::convert_modbus_to_plc,
            commands::calculate_crc16,
            commands::calculate_lrc,
            commands::parse_hex,
            // Project file commands
            commands::save_project_file,
            commands::load_project_file,
            // Serial port commands
            commands::list_serial_ports,
            // Update commands
            update::check_for_update,
            update::install_update,
            update::skip_update,
            update::schedule_update_on_next_launch,
            // Analytics commands
            analytics::get_analytics_enabled,
            analytics::set_analytics_enabled,
        ])
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            if cfg!(debug_assertions) {
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }
            analytics::track_started(app.handle());

            let update_app = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                if let Err(error) = update::install_pending_update(update_app).await {
                    log::warn!("automatic update on launch failed: {error}");
                }
            });
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(|app_handle, event| {
            if let tauri::RunEvent::Exit = event {
                use tauri_plugin_aptabase::EventTracker;
                app_handle.flush_events_blocking();
            }
        });
}
