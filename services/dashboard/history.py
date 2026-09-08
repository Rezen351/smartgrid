from collections import defaultdict


def _add_interval_metrics(rows, tariff_per_kwh, emission_factor):
    total_pv_kwh = 0.0
    total_load_kwh = 0.0
    total_renewable_kwh = 0.0
    for row in rows:
        interval_hours = float(row.get("interval_hours") or 0)
        pv_dc = float(row.get("pv_dc") or 0)
        load_w = float(row.get("load_w") or 0)
        renewable_w = min(
            max(float(row.get("pac_inverter") or 0) + max(float(row.get("p_inverter") or 0), 0), 0),
            load_w,
        )
        total_pv_kwh += pv_dc / 1000 * interval_hours
        total_load_kwh += load_w / 1000 * interval_hours
        interval_renewable_kwh = renewable_w / 1000 * interval_hours
        total_renewable_kwh += interval_renewable_kwh
        row["rf_pct"] = round((renewable_w / load_w) * 100, 2) if load_w > 0.5 else 0
        row["re_saving"] = round(interval_renewable_kwh * tariff_per_kwh, 2)
        row["co2_kg"] = round(interval_renewable_kwh * emission_factor, 4)
    return total_pv_kwh, total_load_kwh, total_renewable_kwh


def _build_summary(rows, totals, tariff_per_kwh, emission_factor, bess_capacity_wh):
    total_pv_kwh, total_load_kwh, total_renewable_kwh = totals
    total_re_saving = total_renewable_kwh * tariff_per_kwh
    avg_rf = (total_renewable_kwh / total_load_kwh) * 100 if total_load_kwh > 0 else 0
    soc_terakhir = float(rows[-1].get("soc") or 0) / 100.0
    load_values = [float(row.get("load_w") or 0) for row in rows if float(row.get("load_w") or 0) > 0.5]
    load_avg_w = sum(load_values) / len(load_values) if load_values else 0
    essa_jam = (bess_capacity_wh * soc_terakhir) / load_avg_w if load_avg_w > 0.5 else 0

    return {
        "total_pv_kwh": round(total_pv_kwh, 3),
        "total_load_kwh": round(total_load_kwh, 3),
        "total_re_saving": round(total_re_saving, 2),
        "avg_rf_pct": round(avg_rf, 2),
        "essa_jam": round(essa_jam, 2),
        "total_co2_kg": round(total_renewable_kwh * emission_factor, 4),
        "total_rows": len(rows),
    }


def _build_charts(rows):
    hourly_pv = defaultdict(float)
    hourly_load = defaultdict(float)
    for row in rows:
        hour = str(row["timestamp"])[:13] + ":00"
        interval_hours = float(row.get("interval_hours") or 0)
        hourly_pv[hour] += float(row.get("pv_dc") or 0) / 1000 * interval_hours
        hourly_load[hour] += float(row.get("load_w") or 0) / 1000 * interval_hours

    sorted_hours = sorted(hourly_pv)
    step = max(1, len(rows) // 500)
    sampled = rows[::step]
    return {
        "hourly_labels": sorted_hours,
        "hourly_pv_kwh": [round(hourly_pv[hour], 3) for hour in sorted_hours],
        "hourly_load_kwh": [round(hourly_load[hour], 3) for hour in sorted_hours],
        "labels": [str(row["timestamp"])[:16] for row in sampled],
        "soc": [float(row.get("soc") or 0) for row in sampled],
        "rf_pct": [float(row.get("rf_pct") or 0) for row in sampled],
    }


def _table_rows(rows):
    table_rows = rows[-200:]
    for row in table_rows:
        row["timestamp"] = str(row["timestamp"])
    return table_rows


def summarize_history_rows(rows, tariff_per_kwh, emission_factor, bess_capacity_wh):
    rows = [dict(row) for row in rows]
    totals = _add_interval_metrics(rows, tariff_per_kwh, emission_factor)
    summary = _build_summary(
        rows, totals, tariff_per_kwh, emission_factor, bess_capacity_wh
    )
    charts = _build_charts(rows)
    table_rows = _table_rows(rows)
    return summary, charts, table_rows
