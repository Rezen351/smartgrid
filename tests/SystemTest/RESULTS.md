# SmartGrid Environment Test Results

| Test | Command / Check | Expected Result |
|---|---|---|
| Docker containers | `docker ps --format '{{.Names}}'` | All `smartgrid-*` containers running |
| nginx | `http://localhost:3001/` | HTTP 200 / reachable |
| mosquitto | TCP `localhost:1883` | Connection accepted |
| influxdb3 | `http://localhost:8181/health` | HTTP 200 / healthy |
| postgres | TCP `localhost:5432` | Connection accepted |
| grafana | `http://localhost:3000/api/health` | HTTP 200 / healthy |
| prometheus | `http://localhost:9090/-/ready` | HTTP 200 / ready |
| cadvisor | `http://localhost:8080/` | HTTP 200 / reachable |
| postgres-exporter | `http://localhost:9187/metrics` | HTTP 200 / metrics exposed |
| MQTT pub/sub | topic `smartgrid/test/system` | Publish/subscribe roundtrip success |

**Result:** 10/10 passed — environment is ready for development.
