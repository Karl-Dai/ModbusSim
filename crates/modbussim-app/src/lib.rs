mod analytics;
mod commands;
mod connection_settings;
mod data_source;
mod frame_tools;
mod mutation;
mod state;
pub mod update;

use state::AppState;
use tauri::Manager;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // tauri-plugin-aptabase 在 setup 中调用 tokio::spawn，需要一个已激活的 Tokio 运行时上下文，
    // 否则启动即 panic（"there is no reactor running"）。进入多线程运行时供其后台轮询任务使用。
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
            // Slave connection commands
            commands::create_slave_connection,
            commands::start_slave_connection,
            commands::stop_slave_connection,
            commands::delete_slave_connection,
            commands::list_slave_connections,
            connection_settings::get_slave_transport,
            connection_settings::update_slave_transport,
            // Slave device commands
            commands::add_slave_device,
            commands::update_slave_device,
            commands::remove_slave_device,
            commands::list_slave_devices,
            // Register commands
            commands::add_register,
            commands::update_register,
            commands::remove_register,
            commands::remove_register,
            commands::read_register,
            commands::read_registers_bulk,
            commands::write_register,
            commands::list_registers,
            commands::export_registers,
            commands::save_text_export,
            commands::import_registers,
            // Log commands
            commands::get_communication_logs,
            commands::clear_communication_logs,
            commands::export_logs_csv,
            // Tool commands
            commands::convert_plc_to_modbus,
            commands::convert_modbus_to_plc,
            commands::calculate_crc16,
            commands::calculate_lrc,
            commands::parse_hex,
            frame_tools::inspect_modbus_frame,
            // State persistence commands
            commands::export_app_state,
            commands::import_app_state,
            commands::clear_app_state,
            // Point-mutation commands
            commands::set_point_mutation,
            commands::clear_point_mutation,
            commands::list_point_mutations,
            commands::set_mutation_running,
            // Project file commands
            commands::save_project_file,
            commands::load_project_file,
            // Serial port commands
            commands::list_serial_ports,
            // Data source commands
            commands::set_data_source,
            commands::remove_data_source,
            commands::list_data_sources,
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

            // Start the single point-mutation tick task.
            let state = app.state::<AppState>();
            mutation::spawn_mutation_tick(
                state.slave_connections.clone(),
                state.mutation_running.clone(),
                state.mutation_runtime.clone(),
            );
            data_source::spawn_data_source_tick(
                app.handle().clone(),
                state.slave_connections.clone(),
                state.data_sources.clone(),
            );
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
