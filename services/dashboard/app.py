from flask import Flask, render_template, jsonify, request, Response, redirect, url_for
from datetime import datetime, timedelta, timezone
import time
import psycopg2
import psycopg2.extras
import os
import jwt
from prometheus_client import Counter, Histogram, generate_latest, CONTENT_TYPE_LATEST
from history import summarize_history_rows

REQUEST_COUNT = Counter('dashboard_http_requests_total', 'Total HTTP requests', ['method', 'endpoint', 'status'])
REQUEST_LATENCY = Histogram('dashboard_http_request_duration_seconds', 'HTTP request latency', ['endpoint'])

try:
    from . import research as research_dashboard
except ImportError:
    import research as research_dashboard

app = Flask(__name__)

@app.before_request
def before_request():
    request.start_time = time.time()

@app.after_request
def after_request(response):
    elapsed = time.time() - request.start_time
    REQUEST_LATENCY.labels(endpoint=request.endpoint).observe(elapsed)
    REQUEST_COUNT.labels(method=request.method, endpoint=request.endpoint, status=response.status_code).inc()
    return response

AUTH_SECRET = os.environ.get("AUTH_SECRET", "")


def _get_token_from_request():
    auth = request.headers.get('Authorization', '')
    if auth.startswith('Bearer '):
        return auth.split(' ', 1)[1]
    return request.cookies.get('access_token')


def _current_user_id():
    token = _get_token_from_request()
    if not token or not AUTH_SECRET:
        return None
    try:
        claims = jwt.decode(token, AUTH_SECRET, algorithms=['HS256'], options={"require": ["exp", "iat"]})
        return claims.get('uid') or claims.get('sub')
    except Exception:
        return None


def login_required(view):
    def wrapper(*args, **kwargs):
        if _current_user_id() is None:
            return redirect(url_for('login_page'))
        return view(*args, **kwargs)
    wrapper.__name__ = view.__name__
    return wrapper


def api_auth_required(view):
    def wrapper(*args, **kwargs):
        if _current_user_id() is None:
            return jsonify({"success": False, "error": {"code": "UNAUTHORIZED", "message": "missing or invalid token"}}), 401
        return view(*args, **kwargs)
    wrapper.__name__ = view.__name__
    return wrapper


DB_CONFIG = {
    "host":     os.environ.get("DB_HOST", "postgres"),
    "port":     int(os.environ.get("DB_PORT", 5432)),
    "dbname":   os.environ.get("DB_NAME", "microgrid_db"),
    "user":     os.environ.get("DB_USER", "microgrid_user"),
    "password": os.environ.get("DB_PASS", "change-me"),
}

# Konstanta sistem
TARIF_PLN_PER_KWH = 955.00
BESS_KAPASITAS_WH = 20_480.0
FAKTOR_EMISI_CO2  = 0.87
MAX_INTERVAL_SECONDS = int(os.environ.get("MAX_INTERVAL_SECONDS", 300))


def get_db():
    return psycopg2.connect(connect_timeout=5, **DB_CONFIG)


def default_data():
    return {
        "pv": 0, "pac_inverter": 0, "pac_estimasi": 0, "load": 0, "soc": 0,
        "grid_va": 0, "p_inverter": 0, "bess_power_dc": 0,
        "freq_grid": 0, "freq_bess": 0, "freq_pv": 0,
        "pv_string_a": 0, "pv_string_b": 0,
        "grid_voltage": 0, "grid_current": 0,
        "dc_voltage": 0, "dc_current": 0, "dc_temperature": 0,
        "efisiensi_rp": 0, "rf_pct": 0, "rf_instan": 0, "lcoe": 0,
        "biaya_pln_murni": 0, "biaya_aktual": 0,
        "essa_jam": 0, "co2_kg": 0,
        "load_estimasi": 0,
        "dss_status": "MENUNGGU DATA",
        "dss_pesan":  "Sistem sedang menginisiasi...",
        "data_status": "NO_DATA",
    }


def _fetch_realtime_data(cursor):
    cursor.execute("""
        SELECT telemetry_id,
               COALESCE(grid_apparent_power_va, grid_pactive) AS grid_apparent_power_va,
               grid_frequency,
               grid_voltage, grid_current,
               dc_meassoc, dc_voltage, dc_current,
               bess_power_dc, dc_temperature,
               p_inverter, ac_frequency,
               a_ms_vol, a_ms_amp, b_ms_vol, b_ms_amp,
               pac_inverter, load_watt, gridms_hz
        FROM sensor_data
        ORDER BY id DESC LIMIT 1
    """)
    row = cursor.fetchone()

    cursor.execute("""
        SELECT COALESCE(SUM(interval_saving_rp), 0) AS efisiensi_biaya_rp,
               COALESCE(SUM(interval_renewable_kwh), 0) AS renewable_kwh,
               COALESCE(SUM(interval_load_kwh), 0) AS load_kwh,
               COALESCE(SUM(interval_co2_kg), 0) AS co2_kg,
               COALESCE((SELECT lcoe_dinamis_rp FROM billing_data ORDER BY id DESC LIMIT 1), 0) AS lcoe_dinamis_rp,
               COALESCE((SELECT essa_jam FROM billing_data ORDER BY id DESC LIMIT 1), 0) AS essa_jam
        FROM billing_data
        WHERE timestamp >= CURRENT_DATE
    """)
    billing = cursor.fetchone()

    if row and row['telemetry_id']:
        cursor.execute("""
            SELECT status_operasi, keputusan_aktif
            FROM control_data
            WHERE telemetry_id = %s
            ORDER BY id DESC LIMIT 1
        """, (row['telemetry_id'],))
    else:
        cursor.execute("""
            SELECT status_operasi, keputusan_aktif
            FROM control_data
            ORDER BY id DESC LIMIT 1
        """)
    control = cursor.fetchone()

    cursor.execute("""
        SELECT
            (SELECT pac_estimasi FROM pv_estimasi
             WHERE timestamp::date = CURRENT_DATE AND timestamp <= NOW()
             ORDER BY timestamp DESC LIMIT 1) AS pac_estimasi,
            (SELECT daya_estimasi FROM load_estimasi
             WHERE timestamp::date = CURRENT_DATE AND timestamp <= NOW()
             ORDER BY timestamp DESC LIMIT 1) AS daya_estimasi
    """)
    return row, billing, control, cursor.fetchone()


def _build_realtime_data(row, billing, control, estimasi_row):
    pv_a = (row['a_ms_vol'] or 0) * (row['a_ms_amp'] or 0)
    pv_b = (row['b_ms_vol'] or 0) * (row['b_ms_amp'] or 0)
    pac_w = row['pac_inverter'] or 0
    p_inv = row['p_inverter'] or 0
    bess_dc = row['bess_power_dc'] or 0
    load_w = row['load_watt'] or 0

    ebt_tersedia = pac_w + max(p_inv, 0)
    ebt_terpakai = min(ebt_tersedia, load_w) if load_w > 0.5 else 0
    ebt_terpakai = max(ebt_terpakai, 0)
    rf_instan = round((ebt_terpakai / load_w) * 100, 2) if load_w > 0.5 else 0

    soc_pct = row['dc_meassoc'] or 0
    essa_rt = (BESS_KAPASITAS_WH * (soc_pct / 100.0)) / load_w if load_w > 0.5 else 0
    return {
        "grid_va":        row['grid_apparent_power_va'] or 0,
        "freq_grid":      row['grid_frequency'] or 0,
        "grid_voltage":   row['grid_voltage']   or 0,
        "grid_current":   row['grid_current']   or 0,
        "soc":            soc_pct,
        "bess_power_dc":  round(bess_dc, 2),
        "dc_voltage":     row['dc_voltage']     or 0,
        "dc_current":     row['dc_current']     or 0,
        "dc_temperature": row['dc_temperature'] or 0,
        "freq_bess":      row['ac_frequency']   or 0,
        "p_inverter":     round(p_inv, 2),
        "pv_string_a":    round(pv_a, 2),
        "pv_string_b":    round(pv_b, 2),
        "pv":             round(pv_a + pv_b, 2),
        "pac_inverter":   pac_w,
        "pac_estimasi":   float(estimasi_row['pac_estimasi']) if estimasi_row and estimasi_row['pac_estimasi'] is not None else None,
        "freq_pv":        row['gridms_hz']      or 0,
        "load":           load_w,
        "load_estimasi":  float(estimasi_row['daya_estimasi']) if estimasi_row and estimasi_row['daya_estimasi'] is not None else None,
        "rf_instan":      rf_instan,
        "efisiensi_rp":    billing['efisiensi_biaya_rp'] if billing else 0,
        "rf_pct":          round((billing['renewable_kwh'] / billing['load_kwh']) * 100, 2) if billing and billing['load_kwh'] else 0,
        "lcoe":            billing['lcoe_dinamis_rp']        if billing else 0,
        "biaya_pln_murni": billing['load_kwh'] * TARIF_PLN_PER_KWH if billing else 0,
        "biaya_aktual":    max((billing['load_kwh'] * TARIF_PLN_PER_KWH) - billing['efisiensi_biaya_rp'], 0) if billing else 0,
        "essa_jam":        round(essa_rt, 2),
        "co2_kg":          float(billing['co2_kg'] or 0)     if billing else 0,
        "dss_status": control['status_operasi']  if control else "MENUNGGU DATA",
        "dss_pesan":  control['keputusan_aktif'] if control else "Menganalisis sistem...",
        "data_status": "OK",
    }


@app.route('/api/data')
@api_auth_required
def api_data():
    try:
        conn   = get_db()
        cursor = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)

        row, billing, control, estimasi_row = _fetch_realtime_data(cursor)
        conn.close()

        if row:
            return jsonify(_build_realtime_data(row, billing, control, estimasi_row))

        return jsonify(default_data())

    except Exception as e:
        print(f"[ERROR] api_data: {e}")
        data = default_data()
        data["data_status"] = "ERROR"
        return jsonify(data)


@app.route('/health/ready')
def health_ready():
    try:
        with get_db() as conn:
            with conn.cursor() as cursor:
                cursor.execute("SELECT 1 FROM sensor_data LIMIT 1")
        return jsonify({"status": "ready"})
    except Exception as e:
        return jsonify({"status": "not_ready", "error": str(e)}), 503


@app.route('/metrics')
def metrics():
    return Response(generate_latest(), mimetype=CONTENT_TYPE_LATEST)


# Historis sensor (60 titik terakhir)
@app.route('/api/sensor/history')
@api_auth_required
def sensor_history():
    try:
        conn   = get_db()
        cursor = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cursor.execute("""
            SELECT timestamp,
                   (a_ms_vol * a_ms_amp)                         AS pv_string_a,
                   (b_ms_vol * b_ms_amp)                         AS pv_string_b,
                   (a_ms_vol * a_ms_amp) + (b_ms_vol * b_ms_amp) AS pv_dc,
                   pac_inverter, load_watt, dc_meassoc,
                   p_inverter,
                   COALESCE(grid_apparent_power_va, grid_pactive) AS grid_power_va
            FROM sensor_data
            ORDER BY id DESC LIMIT 60
        """)
        rows = cursor.fetchall()
        conn.close()
        return jsonify([dict(r) for r in reversed(rows)])
    except Exception as e:
        print(f"[ERROR] sensor_history: {e}")
        return jsonify([])


# Historis keputusan DSS (20 terakhir)
@app.route('/api/control/history')
@api_auth_required
def control_history():
    try:
        conn   = get_db()
        cursor = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cursor.execute("""
            SELECT timestamp, status_operasi,
                   keputusan_aktif, daya_pln_dihitung_watt
            FROM control_data
            ORDER BY id DESC LIMIT 20
        """)
        rows = cursor.fetchall()
        conn.close()
        return jsonify([dict(r) for r in rows])
    except Exception as e:
        print(f"[ERROR] control_history: {e}")
        return jsonify([])


@app.route('/login')
def login_page():
    return render_template('login.html')


@app.route('/logout', methods=['POST'])
def logout_page():
    resp = redirect(url_for('login_page'))
    resp.delete_cookie('access_token')
    resp.delete_cookie('refresh_token')
    return resp


@app.route('/')
@app.route('/dashboard')
@app.route('/dashboard/daily')
@app.route('/dashboard/dispatch')
@app.route('/dashboard/monthly')
@app.route('/dashboard/risk')
@app.route('/dashboard/modules')
@app.route('/modules')
def dashboard_index():
    active_page = request.path.rstrip('/').rsplit('/', 1)[-1]
    if active_page in ('', 'dashboard'):
        active_page = 'daily'
    return render_template('research.html', active_page=active_page)


def _dashboard_api_response(builder):
    try:
        return jsonify(builder())
    except ValueError as exc:
        return jsonify({
            "data_mode": research_dashboard.DATA_MODE,
            "error": str(exc),
        }), 400
    except Exception as exc:
        print(f"[ERROR] dashboard API: {exc}")
        return jsonify({
            "data_mode": research_dashboard.DATA_MODE,
            "error": "Data riset belum dapat diproses.",
        }), 500


def _research_om_rate():
    return request.args.get(
        'om_rate_pct', os.environ.get('RESEARCH_OM_RATE_PCT', '5')
    )


@app.route('/api/dashboard/meta')
@api_auth_required
def api_dashboard_meta():
    return _dashboard_api_response(
        lambda: research_dashboard.get_meta(_research_om_rate())
    )


@app.route('/api/dashboard/daily')
@api_auth_required
def api_dashboard_daily():
    return _dashboard_api_response(lambda: research_dashboard.get_daily(
        date=request.args.get('date', research_dashboard.DEFAULT_DATE),
        scenario=request.args.get('scenario', research_dashboard.DEFAULT_SCENARIO),
        time=request.args.get('time', '12:00'),
        om_rate_pct=_research_om_rate(),
    ))


@app.route('/api/dashboard/dispatch')
@api_auth_required
def api_dashboard_dispatch():
    compare = request.args.get('compare') or None
    return _dashboard_api_response(lambda: research_dashboard.get_dispatch(
        date=request.args.get('date', research_dashboard.DEFAULT_DATE),
        scenario=request.args.get('scenario', research_dashboard.DEFAULT_SCENARIO),
        compare=compare,
    ))


@app.route('/api/dashboard/monthly')
@api_auth_required
def api_dashboard_monthly():
    return _dashboard_api_response(lambda: research_dashboard.get_monthly(
        month=request.args.get('month', research_dashboard.MONTH),
        om_rate_pct=_research_om_rate(),
    ))


@app.route('/api/dashboard/risk')
@api_auth_required
def api_dashboard_risk():
    return _dashboard_api_response(research_dashboard.get_risk)


@app.route('/api/control', methods=['POST'])
@api_auth_required
def api_control():
    body = request.get_json(silent=True)
    if not body or 'action' not in body:
        return jsonify({"status": "error", "pesan": "Field 'action' wajib diisi."}), 400
    return jsonify({
        "status": "not_implemented",
        "pesan": "Command aktuator belum tersedia; endpoint hanya dipertahankan sebagai kontrak API.",
    }), 501


def _history_bounds():
    start_str = request.args.get('start', '')
    end_str = request.args.get('end', '')
    if not start_str or not end_str:
        return None
    return start_str, end_str, start_str + ' 00:00:00', end_str + ' 23:59:59'


@app.route('/api/history')
@api_auth_required
def api_history():
    try:
        bounds = _history_bounds()
        if bounds is None:
            return jsonify({"error": "Parameter start dan end wajib diisi"}), 400
        _, _, start_dt, end_dt = bounds

        conn   = get_db()
        cursor = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)

        cursor.execute("""
            WITH sensor_range AS (
                SELECT s.*,
                       LEAD(s.timestamp) OVER (ORDER BY s.timestamp) AS next_timestamp
                FROM sensor_data s
                WHERE s.timestamp BETWEEN %s AND %s
            )
            SELECT
                s.timestamp,
                ROUND(CAST((s.a_ms_vol * s.a_ms_amp + s.b_ms_vol * s.b_ms_amp) AS numeric), 2) AS pv_dc,
                ROUND(CAST(s.pac_inverter AS numeric), 2) AS pac_inverter,
                ROUND(CAST(COALESCE(s.grid_apparent_power_va, s.grid_pactive) AS numeric), 2) AS grid_va,
                ROUND(CAST(s.p_inverter AS numeric), 2) AS p_inverter,
                ROUND(CAST(s.dc_meassoc AS numeric), 2) AS soc,
                ROUND(CAST(s.load_watt AS numeric), 2) AS load_w,
                CASE
                    WHEN EXTRACT(EPOCH FROM (s.next_timestamp - s.timestamp)) BETWEEN 0 AND %s
                    THEN EXTRACT(EPOCH FROM (s.next_timestamp - s.timestamp)) / 3600.0
                    ELSE 0
                END AS interval_hours,
                COALESCE(c.status_operasi, '-') AS dss_status
            FROM sensor_range s
            LEFT JOIN LATERAL (
                SELECT status_operasi
                FROM control_data c
                WHERE (s.telemetry_id IS NOT NULL AND c.telemetry_id = s.telemetry_id)
                   OR (s.telemetry_id IS NULL AND c.timestamp BETWEEN s.timestamp - INTERVAL '30 seconds' AND s.timestamp + INTERVAL '30 seconds')
                ORDER BY ABS(EXTRACT(EPOCH FROM (c.timestamp - s.timestamp)))
                LIMIT 1
            ) c ON TRUE
            ORDER BY s.timestamp ASC
        """, (start_dt, end_dt, MAX_INTERVAL_SECONDS))

        rows = cursor.fetchall()

        if not rows:
            conn.close()
            return jsonify({"summary": {}, "rows": [], "charts": {}})

        summary, charts, table_rows = summarize_history_rows(
            rows, TARIF_PLN_PER_KWH, FAKTOR_EMISI_CO2, BESS_KAPASITAS_WH
        )

        conn.close()
        return jsonify({
            "summary": summary,
            "charts":  charts,
            "rows":    table_rows,
        })

    except Exception as e:
        print(f"[ERROR] api_history: {e}")
        return jsonify({"error": str(e)}), 500


@app.route('/api/history/export')
@api_auth_required
def export_history_csv():
    import csv, io
    try:
        bounds = _history_bounds()
        if bounds is None:
            return jsonify({"error": "Parameter start dan end wajib diisi"}), 400
        start_str, end_str, start_dt, end_dt = bounds

        conn   = get_db()
        cursor = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cursor.execute("""
            SELECT
                s.timestamp,
                ROUND(CAST((s.a_ms_vol * s.a_ms_amp + s.b_ms_vol * s.b_ms_amp) AS numeric), 2) AS pv_dc_w,
                ROUND(CAST(s.pac_inverter        AS numeric), 2) AS pac_inverter_w,
                ROUND(CAST(COALESCE(s.grid_apparent_power_va, s.grid_pactive) AS numeric), 2) AS grid_va,
                ROUND(CAST(s.p_inverter          AS numeric), 2) AS p_inverter_w,
                ROUND(CAST(s.dc_meassoc          AS numeric), 2) AS soc_pct,
                ROUND(CAST(s.load_watt           AS numeric), 2) AS load_w,
                ROUND(CAST(CASE WHEN s.load_watt > 0.5 THEN
                    LEAST((s.pac_inverter + GREATEST(s.p_inverter, 0)) / s.load_watt * 100, 100)
                    ELSE 0 END AS numeric), 2) AS rf_pct,
                ROUND(CAST(COALESCE(b.interval_saving_rp, 0) AS numeric), 2) AS re_saving_rp,
                COALESCE(c.status_operasi, '-') AS dss_status
            FROM sensor_data s
            LEFT JOIN LATERAL (
                SELECT interval_saving_rp
                FROM billing_data b
                WHERE (s.telemetry_id IS NOT NULL AND b.telemetry_id = s.telemetry_id)
                   OR (s.telemetry_id IS NULL AND b.timestamp BETWEEN s.timestamp - INTERVAL '30 seconds' AND s.timestamp + INTERVAL '30 seconds')
                ORDER BY ABS(EXTRACT(EPOCH FROM (b.timestamp - s.timestamp)))
                LIMIT 1
            ) b ON TRUE
            LEFT JOIN LATERAL (
                SELECT status_operasi
                FROM control_data c
                WHERE (s.telemetry_id IS NOT NULL AND c.telemetry_id = s.telemetry_id)
                   OR (s.telemetry_id IS NULL AND c.timestamp BETWEEN s.timestamp - INTERVAL '30 seconds' AND s.timestamp + INTERVAL '30 seconds')
                ORDER BY ABS(EXTRACT(EPOCH FROM (c.timestamp - s.timestamp)))
                LIMIT 1
            ) c ON TRUE
            WHERE s.timestamp BETWEEN %s AND %s
            ORDER BY s.timestamp ASC
        """, (start_dt, end_dt))
        rows = cursor.fetchall()
        conn.close()

        output = io.StringIO()
        writer = csv.writer(output)
        writer.writerow([
            'Timestamp', 'PV DC (W)', 'AC Inverter PV (W)',
            'Grid PLN (VA)', 'P Inverter Hybrid (W)', 'SoC (%)',
            'Load (W)', 'Renewable Fraction (%)', 'RE Saving (Rp)', 'DSS Status'
        ])
        for r in rows:
            writer.writerow([
                str(r['timestamp']), r['pv_dc_w'], r['pac_inverter_w'],
                r['grid_va'], r['p_inverter_w'], r['soc_pct'],
                r['load_w'], r['rf_pct'], r['re_saving_rp'], r['dss_status']
            ])

        filename = f"microgrid_history_{start_str}_to_{end_str}.csv"
        return Response(
            output.getvalue(),
            mimetype='text/csv',
            headers={'Content-Disposition': f'attachment; filename={filename}'}
        )

    except Exception as e:
        print(f"[ERROR] export_csv: {e}")
        return jsonify({"error": str(e)}), 500


# Status microservice Docker
@app.route('/api/system/services')
@api_auth_required
def api_system_services():
    try:
        import docker
        client = docker.from_env()

        RELEVANT = [
            'mqtt_broker', 'postgres_microgrid',
            'service_sensor', 'service_logger', 'service_billing',
            'service_control', 'service_hmi_flask', 'service_watchdog',
            'service_estimation_pv', 'service_estimation_load',
            'service_pemantauan'
        ]

        containers = client.containers.list(all=True)
        result = []

        for name in RELEVANT:
            matched = next((c for c in containers if c.name == name), None)
            if matched:
                started_at = matched.attrs['State'].get('StartedAt', '')
                status     = matched.status
            else:
                started_at = ''
                status     = 'not found'

            result.append({
                "name":       name,
                "status":     status,
                "started_at": started_at,
            })

        return jsonify(result)

    except Exception as e:
        print(f"[ERROR] api_system_services: {e}")
        return jsonify([]), 500


def _staleness_seconds(timestamp, now=None):
    if not timestamp:
        return None
    wib = timezone(timedelta(hours=7))
    now = now or datetime.now(wib)
    if timestamp.tzinfo is None:
        timestamp = timestamp.replace(tzinfo=wib)
    else:
        timestamp = timestamp.astimezone(wib)
    return round((now - timestamp).total_seconds())


def _status_label(seconds, warning_after=120, stale_after=300):
    if seconds is None:
        return 'NO DATA'
    if seconds < -60:
        return 'FUTURE'
    if seconds < warning_after:
        return 'FRESH'
    if seconds < stale_after:
        return 'WARNING'
    return 'STALE'


def _validity_source(name, timestamp, warning_after=120, stale_after=300):
    return {
        "name": name,
        "timestamp": str(timestamp) if timestamp else '—',
        "staleness_s": _staleness_seconds(timestamp),
        "status": _status_label(
            _staleness_seconds(timestamp), warning_after, stale_after
        ),
    }


@app.route('/api/system/validity')
@api_auth_required
def api_system_validity():
    try:
        conn   = get_db()
        cursor = conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)

        # Staleness check untuk sumber data
        cursor.execute("""
            SELECT timestamp FROM sensor_data ORDER BY id DESC LIMIT 1
        """)
        last_sensor = cursor.fetchone()

        cursor.execute("""
            SELECT timestamp FROM pv_estimasi
            WHERE timestamp <= NOW()
            ORDER BY timestamp DESC LIMIT 1
        """)
        last_pv_est = cursor.fetchone()

        cursor.execute("""
            SELECT timestamp FROM load_estimasi
            WHERE timestamp <= NOW()
            ORDER BY timestamp DESC LIMIT 1
        """)
        last_load_est = cursor.fetchone()

        # Ambil status dan alert dari run validasi terbaru.
        cursor.execute("""
            SELECT timestamp, status_global, jumlah_alert
            FROM monitoring_runs
            ORDER BY id DESC LIMIT 1
        """)
        last_check = cursor.fetchone()

        if last_check:
            cursor.execute("""
                SELECT timestamp, parameter, nilai_aktual,
                       jenis_alert, severity, pesan
                FROM monitoring_alerts
                WHERE timestamp = %s
                ORDER BY id ASC
            """, (last_check['timestamp'],))
            alerts = cursor.fetchall()
        else:
            alerts = []

        conn.close()

        ts_sensor   = last_sensor['timestamp']   if last_sensor   else None
        ts_pv_est   = last_pv_est['timestamp']   if last_pv_est   else None
        ts_load_est = last_load_est['timestamp'] if last_load_est else None

        sources = [
            _validity_source("Sensor (Sunny Boy + Island + Sielis)", ts_sensor),
            _validity_source("Estimasi PV (PIML)", ts_pv_est, 3900, 7200),
            _validity_source("Estimasi Load (DNN)", ts_load_est),
        ]

        check_age = _staleness_seconds(last_check['timestamp']) if last_check else None
        status_global = (
            last_check['status_global']
            if check_age is not None and 0 <= check_age <= 180
            else "UNKNOWN"
        )

        return jsonify({
            "sources":       sources,
            "status_global": status_global,
            "alerts":        [dict(a) for a in alerts],
            "last_check":    str(last_check['timestamp']) if last_check else None,
        })

    except Exception as e:
        print(f"[ERROR] api_system_validity: {e}")
        return jsonify({"sources": [], "status_global": "UNKNOWN", "alerts": []}), 500


if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000, debug=False)
