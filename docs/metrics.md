# Metrics Reference

Metrics are exposed at `/metrics?target=<bmc>` when Prometheus scrapes the exporter.
The default metric name prefix is `idrac_`; it can be changed via `metrics_prefix` in the config file.
Metric groups are individually toggled under the `metrics:` section of the config (or enabled all at once with `all: true`).

**Absent values are never faked as zero.** If a Redfish field is missing or cannot be parsed, the exporter emits no sample for that metric rather than a synthetic 0.

Health-status metrics use a numeric encoding: `0 = OK/GoodInUse`, `1 = Warning`, `2 = Critical`.

---

## Exporter

Emitted unconditionally on every scrape, regardless of which metric groups are enabled.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_exporter_build_info` | untyped | `version`, `revision`, `goversion` (const) | Constant metric with build information for the exporter |
| `idrac_exporter_scrape_errors_total` | counter | — | Total number of errors encountered while scraping target |

---

## System

Enabled by `metrics.system: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_system_power_on` | gauge | — | Power state of the system |
| `idrac_system_health` | gauge | `status` | Health status of the system |
| `idrac_system_indicator_led_on` | gauge | `state` | Indicator LED state of the system (deprecated in Redfish) |
| `idrac_system_indicator_active` | gauge | — | State of the system location indicator |
| `idrac_system_memory_size_bytes` | gauge | — | Total memory size of the system in bytes |
| `idrac_system_cpu_count` | gauge | `model` | Total number of CPUs in the system |
| `idrac_system_bios_info` | untyped | `version` | Information about the BIOS |
| `idrac_system_machine_info` | untyped | `manufacturer`, `model`, `serial`, `sku`, `hostname` | Information about the machine |

---

## Sensors

Enabled by `metrics.sensors: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_sensors_temperature` | gauge | `id`, `name`, `units` | Sensors reporting temperature measurements |
| `idrac_sensors_fan_health` | gauge | `id`, `name`, `status` | Health status for fans |
| `idrac_sensors_fan_speed` | gauge | `id`, `name`, `units` | Sensors reporting fan speed measurements |
| `idrac_sensors_voltage` | gauge | `id`, `name`, `units` | Sensors reporting voltage measurements |

> **Note:** Voltage sensors come from the legacy Redfish `Power` resource. When the power group is also enabled, voltage is emitted as part of power collection. When only the sensors group is enabled, voltage is fetched separately.

---

## Power Supply

Enabled by `metrics.power: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_power_supply_health` | gauge | `id`, `status` | Power supply health status |
| `idrac_power_supply_output_watts` | gauge | `id` | Power supply output in watts |
| `idrac_power_supply_input_watts` | gauge | `id` | Power supply input in watts |
| `idrac_power_supply_capacity_watts` | gauge | `id` | Power supply capacity in watts |
| `idrac_power_supply_input_voltage` | gauge | `id` | Power supply input voltage |
| `idrac_power_supply_efficiency_percent` | gauge | `id` | Power supply efficiency in percentage |

---

## Power Control

Enabled by `metrics.power: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_power_control_consumed_watts` | gauge | `id`, `name` | Consumption of power control system in watts |
| `idrac_power_control_capacity_watts` | gauge | `id`, `name` | Capacity of power control system in watts |
| `idrac_power_control_min_consumed_watts` | gauge | `id`, `name` | Minimum consumption of power control system during the reported interval |
| `idrac_power_control_max_consumed_watts` | gauge | `id`, `name` | Maximum consumption of power control system during the reported interval |
| `idrac_power_control_avg_consumed_watts` | gauge | `id`, `name` | Average consumption of power control system during the reported interval |
| `idrac_power_control_interval_in_minutes` | gauge | `id`, `name` | Interval for measurements of power control system |

---

## System Event Log

Enabled by `metrics.events: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_events_log_entry` | counter | `id`, `message`, `severity` | Entry from the system event log |

> The counter value is the Unix timestamp of when the event was created.

---

## Storage

Enabled by `metrics.storage: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_storage_info` | untyped | `id`, `name` | Information about storage sub systems |
| `idrac_storage_health` | gauge | `id`, `status` | Health status for storage sub systems |
| `idrac_storage_drive_info` | untyped | `id`, `storage_id`, `manufacturer`, `mediatype`, `model`, `name`, `protocol`, `serial`, `slot` | Information about disk drives |
| `idrac_storage_drive_health` | gauge | `id`, `storage_id`, `status` | Health status for disk drives |
| `idrac_storage_drive_capacity_bytes` | gauge | `id`, `storage_id` | Capacity of disk drives in bytes |
| `idrac_storage_drive_life_left_percent` | gauge | `id`, `storage_id` | Predicted life left in percent |
| `idrac_storage_drive_indicator_active` | gauge | `id`, `storage_id` | State of the drive location indicator |
| `idrac_storage_controller_info` | untyped | `id`, `storage_id`, `manufacturer`, `model`, `name`, `firmware` | Information about storage controllers |
| `idrac_storage_controller_health` | gauge | `id`, `storage_id`, `status` | Health status for storage controllers |
| `idrac_storage_controller_speed_mbps` | gauge | `id`, `storage_id` | Speed of storage controllers in Mbps |
| `idrac_storage_controller_cache_size_bytes` | gauge | `id`, `storage_id` | Size of the controller cache in bytes |
| `idrac_storage_controller_cache_health` | gauge | `id`, `storage_id`, `status` | Health status for the storage controller cache |
| `idrac_storage_volume_info` | untyped | `id`, `storage_id`, `name`, `volumetype`, `raidtype` | Information about virtual volumes |
| `idrac_storage_volume_health` | gauge | `id`, `storage_id`, `status` | Health status for virtual volumes |
| `idrac_storage_volume_media_span_count` | gauge | `id`, `storage_id` | Number of media spanned by virtual volumes |
| `idrac_storage_volume_capacity_bytes` | gauge | `id`, `storage_id` | Capacity of virtual volumes in bytes |

---

## Memory

Enabled by `metrics.memory: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_memory_module_info` | untyped | `id`, `ecc`, `manufacturer`, `type`, `name`, `serial`, `rank` | Information about memory modules |
| `idrac_memory_module_health` | gauge | `id`, `status` | Health status for memory modules |
| `idrac_memory_module_capacity_bytes` | gauge | `id` | Capacity of memory modules in bytes |
| `idrac_memory_module_speed_mhz` | gauge | `id` | Speed of memory modules in Mhz |

---

## Network

Enabled by `metrics.network: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_network_adapter_info` | untyped | `id`, `manufacturer`, `model`, `serial` | Information about network adapters |
| `idrac_network_adapter_health` | gauge | `id`, `status` | Health status for network adapters |
| `idrac_network_port_health` | gauge | `id`, `adapter_id`, `status` | Health status for network ports |
| `idrac_network_port_max_speed_mbps` | gauge | `id`, `adapter_id` | Max link speed of network ports in Mbps |
| `idrac_network_port_current_speed_mbps` | gauge | `id`, `adapter_id` | Current link speed of network ports in Mbps |
| `idrac_network_port_link_up` | gauge | `id`, `adapter_id`, `status` | Link status of network ports (up or down) |

---

## Processors

Enabled by `metrics.processors: true`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_cpu_info` | untyped | `id`, `socket`, `manufacturer`, `model`, `arch` | Information about the CPU |
| `idrac_cpu_health` | gauge | `id`, `status` | Health status of the CPU |
| `idrac_cpu_voltage` | gauge | `id` | Current voltage of the CPU |
| `idrac_cpu_max_speed_mhz` | gauge | `id` | Maximum speed of the CPU in Mhz |
| `idrac_cpu_current_speed_mhz` | gauge | `id` | Current speed of the CPU in Mhz |
| `idrac_cpu_total_cores` | gauge | `id` | Total number of CPU cores |
| `idrac_cpu_total_threads` | gauge | `id` | Total number of CPU threads |

---

## Manager

Enabled by `metrics.manager: true`. The manager is the BMC itself (e.g., iDRAC, iLO, XClarity).

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_manager_info` | untyped | `id`, `type`, `model`, `firmware` | Information about the manager |
| `idrac_manager_health` | gauge | `id`, `status` | Health status of the manager |

---

## Dell OEM

Enabled by `metrics.extra: true`. These metrics are only emitted on Dell systems.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_dell_battery_rollup_health` | gauge | `status` | Health rollup status for the batteries |
| `idrac_dell_estimated_system_airflow_cfm` | gauge | — | Estimated system airflow in cubic feet per minute |
| `idrac_dell_controller_battery_health` | gauge | `id`, `storage_id`, `name`, `status` | Health status of storage controller battery |

---

## PDU

Emitted only when the target is a Redfish Power Distribution Unit (PDU). PDU metrics replace server metrics — a target is either a server or a PDU, not both. PDU collection does not require a specific `metrics:` toggle; it is activated automatically when `RackPDUs` are discovered at the Redfish root.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `idrac_pdu_info` | untyped | `id`, `firmware`, `manufacturer`, `model`, `name`, `serial`, `type` | Information about the PDU |
| `idrac_pdu_health` | gauge | `id`, `status` | Health status of the PDU |
| `idrac_pdu_power_watts` | gauge | `id` | Power reading in watts |
| `idrac_pdu_power_apparent_va` | gauge | `id` | Apparent power reading in VA units |
| `idrac_pdu_power_factor` | gauge | `id` | Power factor (efficiency) |
| `idrac_pdu_energy_kwh` | gauge | `id` | Energy consumption in kWh |
