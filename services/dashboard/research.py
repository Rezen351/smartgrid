"""Mesin data DEMO/SIMULATION dashboard riset yang terisolasi dari operasi.

Angka agregat berasal dari laporan, sedangkan seluruh titik 15 menit direkonstruksi
secara deterministik. Modul ini sengaja tidak mengimpor Flask, MQTT, atau driver basis
data. Sumber laporan memakai kW/kWh; konversi ini berhenti pada batas modul riset dan
tidak mengubah kontrak telemetri runtime yang memakai Watt.
"""

from __future__ import annotations

import csv
import json
import math
import random
from copy import deepcopy
from datetime import date as Date
from datetime import datetime, timedelta
from pathlib import Path


DATA_MODE = "DEMO/SIMULATION"
FIXED_SEED = 20250125
INTERVAL_MINUTES = 15
INTERVAL_HOURS = INTERVAL_MINUTES / 60.0
MONTH = "2025-01"
SCENARIOS = ("baseline", "hemat", "hemat_plus")
DEFAULT_DATE = "2025-01-25"
DEFAULT_SCENARIO = "hemat_plus"
MONTHLY_LOAD_KWH = 41250.0
TRANSITION_PENALTY_IDR = 50_000.0
MAX_OM_RATE_PCT = 1000.0
BATTERY_CAPACITY_KWH = 240.0
DATA_DIR = Path(__file__).resolve().parent / "data" / "research"

SOC_BOUNDS = {
    "baseline": (10.0, 95.0),
    "hemat": (10.0, 95.0),
    "hemat_plus": (15.0, 99.0),
}


def _load_sources():
    with (DATA_DIR / "report_targets.json").open(encoding="utf-8") as handle:
        report = json.load(handle)
    daily = {scenario: {} for scenario in SCENARIOS}
    with (DATA_DIR / "daily_targets.csv").open(encoding="utf-8", newline="") as handle:
        for row in csv.DictReader(handle):
            scenario = row.pop("scenario")
            day = row.pop("date")
            daily[scenario][day] = {key: float(value) for key, value in row.items()}
    return report, daily


_REPORT, _DAILY = _load_sources()


def _validate_scenario(scenario):
    if scenario not in SCENARIOS:
        raise ValueError("scenario must be one of: baseline, hemat, hemat_plus")


def _validate_date(day):
    if not isinstance(day, str):
        raise ValueError("date must be an ISO string in January 2025")
    try:
        parsed = Date.fromisoformat(day)
    except ValueError as exc:
        raise ValueError("date must be an ISO string in January 2025") from exc
    if parsed.year != 2025 or parsed.month != 1:
        raise ValueError("date must be within 2025-01-01 through 2025-01-31")
    return parsed


def _validate_time(value):
    if not isinstance(value, str):
        raise ValueError("time must be HH:MM on a 15-minute cadence")
    try:
        parsed = datetime.strptime(value, "%H:%M")
    except ValueError as exc:
        raise ValueError("time must be HH:MM on a 15-minute cadence") from exc
    if parsed.minute % INTERVAL_MINUTES:
        raise ValueError("time must be HH:MM on a 15-minute cadence")
    return parsed.hour * 4 + parsed.minute // 15


def _validate_om_rate(value):
    try:
        rate = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError("om_rate_pct must be a finite nonnegative number") from exc
    if not math.isfinite(rate) or rate < 0 or rate > MAX_OM_RATE_PCT:
        raise ValueError(
            f"om_rate_pct must be between 0 and {MAX_OM_RATE_PCT:g}"
        )
    return rate


def _timestamps(day):
    start = datetime.fromisoformat(day)
    return [(start + timedelta(minutes=15 * index)).isoformat(timespec="minutes") for index in range(96)]


def _weather(day):
    if day == "2025-01-11":
        return "cloudy"
    if day == "2025-01-25":
        return "clear"
    pv = _DAILY["hemat_plus"][day]["pv_absorbed_kwh"]
    if pv < 300:
        return "cloudy"
    if pv >= 475:
        return "clear"
    return "variable"


def _scaled_curve(weights, energy_kwh):
    denominator = sum(weights) * INTERVAL_HOURS
    if denominator == 0:
        return [0.0] * len(weights)
    scale = energy_kwh / denominator
    values = [weight * scale for weight in weights]
    # Keep the integral auditable despite binary floating-point accumulation.
    correction = energy_kwh - sum(values) * INTERVAL_HOURS
    values[max(range(len(values)), key=values.__getitem__)] += correction / INTERVAL_HOURS
    return values


def _load_daily_targets():
    """Allocate the exact 41,250 kWh monthly load to plausible daily shapes."""
    weights = []
    for day in sorted(_DAILY["baseline"]):
        target = _DAILY["baseline"][day]
        weights.append(target["generator_energy_kwh"] + target["pv_absorbed_kwh"] - target["ens_kwh"])
    scale = MONTHLY_LOAD_KWH / sum(weights)
    result = {day: weight * scale for day, weight in zip(sorted(_DAILY["baseline"]), weights)}
    last = sorted(result)[-1]
    result[last] += MONTHLY_LOAD_KWH - sum(result.values())
    return result


_LOAD_DAILY_KWH = _load_daily_targets()


def _load_curve(day, energy_kwh):
    parsed = Date.fromisoformat(day)
    weekend = parsed.weekday() >= 5
    rng = random.Random(FIXED_SEED + parsed.day * 101)
    weights = []
    for index in range(96):
        hour = index / 4.0
        morning = 11.0 * math.exp(-((hour - 8.0) / 2.3) ** 2)
        evening = 28.0 * math.exp(-((hour - 18.0) / 3.2) ** 2)
        base = 39.0 + morning + evening - (3.0 if weekend else 0.0)
        weights.append(max(base * (1.0 + rng.uniform(-0.025, 0.025)), 1.0))
    return _scaled_curve(weights, energy_kwh)


def _pv_curve(day, scenario, energy_kwh):
    parsed = Date.fromisoformat(day)
    weather = _weather(day)
    rng = random.Random(FIXED_SEED + parsed.day * 1009 + SCENARIOS.index(scenario) * 17)
    weights = []
    for index in range(96):
        hour = index / 4.0
        solar = math.sin(math.pi * (hour - 6.0) / 12.0) if 6.0 <= hour <= 18.0 else 0.0
        if solar <= 0:
            weights.append(0.0)
            continue
        cloud = 1.0
        if weather == "cloudy":
            cloud = 0.55 + 0.18 * math.sin(index * 1.7) + rng.uniform(-0.08, 0.08)
        elif weather == "variable":
            cloud = 0.82 + 0.10 * math.sin(index * 0.91) + rng.uniform(-0.04, 0.04)
        weights.append(max(solar ** 1.35 * cloud, 0.05))
    return _scaled_curve(weights, energy_kwh)


def _genset_curve(day, target, pv_curve, load_curve):
    on_count = int(round(target["generator_hours"] / INTERVAL_HOURS))
    # Prefer operation outside strong PV periods while retaining exactly the report hours.
    scores = []
    for index in range(96):
        hour = index / 4.0
        night_bonus = 50.0 if hour < 6.0 or hour >= 18.0 else 0.0
        scores.append((night_bonus + load_curve[index] - 0.8 * pv_curve[index], index))
    on_indices = {index for _, index in sorted(scores, reverse=True)[:on_count]}
    weights = [max(load_curve[index] - 0.25 * pv_curve[index], 20.0) if index in on_indices else 0.0 for index in range(96)]
    return _scaled_curve(weights, target["generator_energy_kwh"])


def _battery_and_soc(day, scenario):
    low, high = SOC_BOUNDS[scenario]
    span = high - low
    weather_factor = {"cloudy": 0.34, "variable": 0.58, "clear": 0.78}[_weather(day)]
    amplitude = span * weather_factor * (0.42 if scenario != "hemat_plus" else 0.48)
    center = low + span * (0.50 if scenario != "hemat_plus" else 0.54)
    soc = []
    for index in range(96):
        # Charge in the morning, peak near noon, discharge into the evening.
        phase = 2.0 * math.pi * (index / 96.0 - 0.25)
        value = center + amplitude * math.sin(phase)
        soc.append(min(high, max(low, value)))
    battery = []
    for index in range(96):
        next_soc = soc[(index + 1) % 96]
        # Positive is discharge; negative is charging, matching p_inverter convention.
        battery.append(-((next_soc - soc[index]) / 100.0) * BATTERY_CAPACITY_KWH / INTERVAL_HOURS)
    return battery, soc


def _build_dispatch(day, scenario):
    target = _DAILY[scenario][day]
    load = _load_curve(day, _LOAD_DAILY_KWH[day])
    pv = _pv_curve(day, scenario, target["pv_absorbed_kwh"])
    genset = _genset_curve(day, target, pv, load)
    battery, soc = _battery_and_soc(day, scenario)
    low, high = SOC_BOUNDS[scenario]
    return {
        "data_mode": DATA_MODE,
        "reconstructed": True,
        "provenance": "Kurva 15 menit direkonstruksi deterministik dari agregat laporan; bukan pengukuran.",
        "date": day,
        "scenario": scenario,
        "weather": _weather(day),
        "interval_minutes": INTERVAL_MINUTES,
        "timestamps": _timestamps(day),
        "load_kw": [round(value, 6) for value in load],
        "genset_kw": [round(value, 6) for value in genset],
        "pv_absorbed_kw": [round(value, 6) for value in pv],
        "battery_kw": [round(value, 6) for value in battery],
        "soc_pct": [round(value, 6) for value in soc],
        "soc_bounds_pct": {"min": low, "max": high},
        "battery_sign_convention": "positive=discharge, negative=charge",
        "power_balance_enforced": False,
        "battery_efficiency_applied": False,
        "units": {"power": "kW", "energy": "kWh", "soc": "%"},
    }


def _cost_breakdown(fuel_cost, ens_penalty, transition_penalty, om_rate_pct):
    source_total = fuel_cost + ens_penalty + transition_penalty
    om_cost = fuel_cost * om_rate_pct / 100.0
    return {
        "source_cost": {
            "fuel_cost_idr": round(fuel_cost, 2),
            "ens_penalty_idr": round(ens_penalty, 2),
            "transition_penalty_idr": round(transition_penalty, 2),
            "soc_penalty_idr": 0.0,
            "total_idr": round(source_total, 2),
            "om_included": False,
        },
        "demo_cost_with_om": {
            "om_rate_pct_of_fuel_cost": om_rate_pct,
            "om_cost_idr": round(om_cost, 2),
            "total_idr": round(source_total + om_cost, 2),
            "source_total_idr": round(source_total, 2),
        },
    }


def _allocated_daily_report(day, scenario, om_rate_pct):
    target = _DAILY[scenario][day]
    monthly = _REPORT["monthly"][scenario]
    energy_total = sum(row["generator_energy_kwh"] for row in _DAILY[scenario].values())
    energy_ratio = target["generator_energy_kwh"] / energy_total
    fuel_l = monthly["fuel_l"] * energy_ratio
    days = sorted(_DAILY[scenario])

    def allocate_money(total, weight_key):
        weights = [_DAILY[scenario][candidate][weight_key] for candidate in days]
        weight_total = sum(weights)
        allocated = {}
        running = 0.0
        for candidate, weight in zip(days[:-1], weights[:-1]):
            amount = round(total * weight / weight_total, 2) if weight_total else 0.0
            allocated[candidate] = amount
            running += amount
        allocated[days[-1]] = round(total - running, 2)
        return allocated[day]

    fuel_cost = allocate_money(monthly["fuel_cost_idr"], "generator_energy_kwh")
    ens_penalty = allocate_money(monthly["ens_penalty_idr"], "ens_kwh")
    day_number = int(day[-2:])
    # Report only gives monthly transition totals; spread deterministically for daily display.
    starts = monthly["starts"] // 31 + (1 if day_number <= monthly["starts"] % 31 else 0)
    stops = monthly["stops"] // 31 + (1 if day_number <= monthly["stops"] % 31 else 0)
    transition_penalty = (starts + stops) * TRANSITION_PENALTY_IDR
    costs = _cost_breakdown(
        fuel_cost, ens_penalty, transition_penalty, om_rate_pct
    )
    return {
        "load_kwh": _LOAD_DAILY_KWH[day],
        "fuel_l": fuel_l,
        "generator_hours": target["generator_hours"],
        "generator_energy_kwh": target["generator_energy_kwh"],
        "pv_absorbed_kwh": target["pv_absorbed_kwh"],
        "curtailment_kwh": target["curtailment_kwh"],
        "ens_kwh": target["ens_kwh"],
        "starts_allocated": starts,
        "stops_allocated": stops,
        "costs": costs,
    }


def _cumulative(dispatch, final_kpi, selected_index):
    end = selected_index + 1
    generator_energy = sum(dispatch["genset_kw"][:end]) * INTERVAL_HOURS
    pv_energy = sum(dispatch["pv_absorbed_kw"][:end]) * INTERVAL_HOURS
    load_energy = sum(dispatch["load_kw"][:end]) * INTERVAL_HOURS
    gen_ratio = generator_energy / final_kpi["generator_energy_kwh"] if final_kpi["generator_energy_kwh"] else 0.0
    elapsed_ratio = end / 96.0
    return {
        "through_time": dispatch["timestamps"][selected_index],
        "load_kwh": round(load_energy, 6),
        "generator_energy_kwh": round(generator_energy, 6),
        "generator_hours": round(sum(value > 0 for value in dispatch["genset_kw"][:end]) * INTERVAL_HOURS, 2),
        "pv_absorbed_kwh": round(pv_energy, 6),
        "fuel_l": round(final_kpi["fuel_l"] * gen_ratio, 6),
        "ens_kwh": round(final_kpi["ens_kwh"] * elapsed_ratio, 9),
        "curtailment_kwh": round(final_kpi["curtailment_kwh"] * elapsed_ratio, 6),
    }


def get_meta(om_rate_pct=5):
    """Return identity, constraints, defaults, and provenance for the demo engine."""
    om_rate = _validate_om_rate(om_rate_pct)
    return {
        "data_mode": DATA_MODE,
        "identity": "Lab Energy Management ITB",
        "isolated_from_operational_data": True,
        "reconstructed": True,
        "period": MONTH,
        "dates": [f"2025-01-{day:02d}" for day in range(1, 32)],
        "scenarios": list(SCENARIOS),
        "defaults": {"date": DEFAULT_DATE, "scenario": DEFAULT_SCENARIO, "time": "12:00", "om_rate_pct": om_rate},
        "reference_model": {"pv_kw": 120, "battery_kwh": 240, "generator_kw": 120},
        "cadence_minutes": INTERVAL_MINUTES,
        "fixed_seed": FIXED_SEED,
        "dataset": {
            "daily_target_rows": len(SCENARIOS) * 31,
            "reconstructed_interval_points": len(SCENARIOS) * 31 * 96,
            "scenario_count": len(SCENARIOS),
            "source_files": ["daily_targets.csv", "report_targets.json"],
        },
        "battery_sign_convention": "positive=discharge, negative=charge",
        "power_balance_enforced": False,
        "battery_efficiency_applied": False,
        "soc_bounds_pct": {key: {"min": value[0], "max": value[1]} for key, value in SOC_BOUNDS.items()},
        "provenance": {
            "aggregates": "Laporan Luaran 1, tabel 5.6-2 dan 5.6-3",
            "curves": "Rekonstruksi deterministik; bukan pengukuran",
        },
    }


def get_dispatch(date=DEFAULT_DATE, scenario=DEFAULT_SCENARIO, compare=None):
    """Return one reconstructed 96-point dispatch and an optional comparison."""
    _validate_date(date)
    _validate_scenario(scenario)
    if compare is not None:
        _validate_scenario(compare)
    result = _build_dispatch(date, scenario)
    if compare is not None:
        result["comparison"] = _build_dispatch(date, compare)
    return result


def get_daily(date=DEFAULT_DATE, scenario=DEFAULT_SCENARIO, time="23:45", om_rate_pct=5):
    """Return selected-time snapshot, cumulative KPI, final KPI, and comparisons."""
    _validate_date(date)
    _validate_scenario(scenario)
    selected_index = _validate_time(time)
    om_rate = _validate_om_rate(om_rate_pct)
    dispatch = _build_dispatch(date, scenario)
    final_kpi = _allocated_daily_report(date, scenario, om_rate)
    snapshot = {
        "timestamp": dispatch["timestamps"][selected_index],
        "load_kw": dispatch["load_kw"][selected_index],
        "genset_kw": dispatch["genset_kw"][selected_index],
        "pv_absorbed_kw": dispatch["pv_absorbed_kw"][selected_index],
        "battery_kw": dispatch["battery_kw"][selected_index],
        "soc_pct": dispatch["soc_pct"][selected_index],
        "genset_status": "ON" if dispatch["genset_kw"][selected_index] > 0 else "OFF",
        "battery_mode": "discharge" if dispatch["battery_kw"][selected_index] > 0 else ("charge" if dispatch["battery_kw"][selected_index] < 0 else "idle"),
    }
    comparisons = {}
    for candidate in SCENARIOS:
        candidate_kpi = _allocated_daily_report(date, candidate, om_rate)
        comparisons[candidate] = {
            key: candidate_kpi[key]
            for key in ("fuel_l", "generator_hours", "pv_absorbed_kwh", "curtailment_kwh", "ens_kwh")
        }
        comparisons[candidate]["source_total_cost_idr"] = candidate_kpi["costs"]["source_cost"]["total_idr"]
        comparisons[candidate]["demo_total_with_om_idr"] = candidate_kpi["costs"]["demo_cost_with_om"]["total_idr"]
    return {
        "data_mode": DATA_MODE,
        "reconstructed": True,
        "date": date,
        "scenario": scenario,
        "weather": _weather(date),
        "selected_time": time,
        "snapshot": snapshot,
        "cumulative_kpi": _cumulative(dispatch, final_kpi, selected_index),
        "final_daily_kpi": final_kpi,
        "scenario_comparisons": comparisons,
        "provenance": (
            "Energi harian dari tabel 5.6-3; BBM, beban, transisi, dan biaya "
            "dialokasikan dari total bulanan. Kurva direkonstruksi independen, "
            "tidak menegakkan neraca daya, dan bukan pengukuran."
        ),
    }


def get_monthly(month=MONTH, om_rate_pct=5):
    """Return exact January report targets and report-derived daily chart rows."""
    if month != MONTH:
        raise ValueError("month must be 2025-01")
    om_rate = _validate_om_rate(om_rate_pct)
    scenarios = {}
    daily_series = {}
    for scenario in SCENARIOS:
        source = deepcopy(_REPORT["monthly"][scenario])
        om_cost = source["fuel_cost_idr"] * om_rate / 100.0
        scenarios[scenario] = {
            "source_report": source,
            "demo_cost_with_om": {
                "om_rate_pct_of_fuel_cost": om_rate,
                "om_cost_idr": round(om_cost, 2),
                "source_total_idr": source["source_total_cost_idr"],
                "total_idr": round(source["source_total_cost_idr"] + om_cost, 2),
            },
        }
        daily_series[scenario] = [
            {"date": day, **deepcopy(_DAILY[scenario][day])}
            for day in sorted(_DAILY[scenario])
        ]
    return {
        "data_mode": DATA_MODE,
        "reconstructed": True,
        "month": month,
        "load_target_kwh": MONTHLY_LOAD_KWH,
        "scenarios": scenarios,
        "daily_series": daily_series,
        "aggregate_tolerance": {"energy_kwh": 0.001, "generator_hours": 0.0},
        "provenance": "Target bulanan eksak dari tabel 5.6-2; seri harian dari tabel 5.6-3; titik interval direkonstruksi.",
    }


def get_risk():
    """Return report risk metrics plus a deterministic reconstructed histogram."""
    risk = deepcopy(_REPORT["risk"])
    rng = random.Random(FIXED_SEED + 513)
    samples = []
    for _ in range(risk["scenario_count"]):
        cluster = rng.random()
        center = 410000 if cluster < 0.32 else (545000 if cluster < 0.72 else 690000)
        samples.append(max(240000, min(1_020_000, rng.gauss(center, 48000))))
    edges = [250000 + index * 50000 for index in range(17)]
    counts = [0] * (len(edges) - 1)
    for value in samples:
        index = min(max(int((value - edges[0]) // 50000), 0), len(counts) - 1)
        counts[index] += 1
    return {
        "data_mode": DATA_MODE,
        "reconstructed": True,
        "summary": {
            "mean_cost_idr": risk["mean_cost_idr"],
            "var95_cost_idr": risk["var95_cost_idr"],
            "cvar95_cost_idr": risk["cvar95_cost_idr"],
            "reserve_idr": risk["reserve_idr"],
        },
        "parameters": {key: risk[key] for key in ("scenario_count", "alpha", "pv_sigma", "fuel_price_idr_per_l", "start_penalty_idr")},
        "histogram": {
            "reconstructed": True,
            "bin_edges_idr": edges,
            "counts": counts,
            "markers_idr": {"mean": risk["mean_cost_idr"], "var95": risk["var95_cost_idr"], "cvar95": risk["cvar95_cost_idr"]},
            "note": "Bentuk histogram direkonstruksi dengan fixed seed; sampel mentah laporan tidak tersedia.",
        },
        "provenance": {"document": risk["source_document"], "section": risk["section"]},
    }


__all__ = ["get_meta", "get_daily", "get_dispatch", "get_monthly", "get_risk"]
