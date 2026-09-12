"""
Data Generator untuk SmartGrid Energy Dispatch & Switching
Mengekstrak dan merekonstruksi dataset time-series 15-menit dari target riset SmartGrid.
Menghasilkan data historis lengkap (Load, PV, Baterai SOC, Genset Status).
"""

import os
import csv
import json
import math
import random
from pathlib import Path
from datetime import datetime, date as Date, timedelta

FIXED_SEED = 20250125
INTERVAL_MINUTES = 15
INTERVAL_HOURS = INTERVAL_MINUTES / 60.0
SCENARIOS = ("baseline", "hemat", "hemat_plus")
MONTHLY_LOAD_KWH = 41250.0
BATTERY_CAPACITY_KWH = 240.0

SOC_BOUNDS = {
    "baseline": (10.0, 95.0),
    "hemat": (10.0, 95.0),
    "hemat_plus": (15.0, 99.0),
}

# Lokasi sumber data dari repo smartgrid
BASE_DIR = Path(__file__).resolve().parent
candidates = [
    BASE_DIR.parent / "dashboard" / "data" / "research",
    BASE_DIR.parent.parent / "services" / "dashboard" / "data" / "research",
    BASE_DIR.parent / "smartgrid" / "services" / "dashboard" / "data" / "research",
]
RESEARCH_DIR = next((p for p in candidates if (p / "daily_targets.csv").exists()), candidates[0])


def load_research_targets():
    """Membaca daily_targets.csv dari folder riset dashboard."""
    daily_file = RESEARCH_DIR / "daily_targets.csv"
    if not daily_file.exists():
        raise FileNotFoundError(f"File {daily_file} tidak ditemukan.")

    daily = {scenario: {} for scenario in SCENARIOS}
    with open(daily_file, "r", encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            scenario = row.pop("scenario")
            day = row.pop("date")
            daily[scenario][day] = {k: float(v) for k, v in row.items()}
    return daily


def get_weather(day, daily_data):
    if day == "2025-01-11":
        return "cloudy"
    if day == "2025-01-25":
        return "clear"
    pv = daily_data["hemat_plus"][day]["pv_absorbed_kwh"]
    if pv < 300:
        return "cloudy"
    if pv >= 475:
        return "clear"
    return "variable"


def scaled_curve(weights, energy_kwh):
    denominator = sum(weights) * INTERVAL_HOURS
    if denominator == 0:
        return [0.0] * len(weights)
    scale = energy_kwh / denominator
    values = [weight * scale for weight in weights]
    correction = energy_kwh - sum(values) * INTERVAL_HOURS
    max_idx = max(range(len(values)), key=values.__getitem__)
    values[max_idx] += correction / INTERVAL_HOURS
    return values


def compute_daily_load_targets(daily_data):
    weights = []
    days = sorted(daily_data["baseline"].keys())
    for day in days:
        target = daily_data["baseline"][day]
        weights.append(target["generator_energy_kwh"] + target["pv_absorbed_kwh"] - target["ens_kwh"])
    scale = MONTHLY_LOAD_KWH / sum(weights)
    result = {day: weight * scale for day, weight in zip(days, weights)}
    last_day = days[-1]
    result[last_day] += MONTHLY_LOAD_KWH - sum(result.values())
    return result


def generate_load_curve(day, energy_kwh):
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
    return scaled_curve(weights, energy_kwh)


def generate_pv_curve(day, scenario, energy_kwh, weather):
    parsed = Date.fromisoformat(day)
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
    return scaled_curve(weights, energy_kwh)


def generate_genset_curve(target, pv_curve, load_curve):
    on_count = int(round(target["generator_hours"] / INTERVAL_HOURS))
    scores = []
    for index in range(96):
        hour = index / 4.0
        night_bonus = 50.0 if hour < 6.0 or hour >= 18.0 else 0.0
        scores.append((night_bonus + load_curve[index] - 0.8 * pv_curve[index], index))
    on_indices = {index for _, index in sorted(scores, reverse=True)[:on_count]}
    weights = [
        max(load_curve[index] - 0.25 * pv_curve[index], 20.0) if index in on_indices else 0.0
        for index in range(96)
    ]
    return scaled_curve(weights, target["generator_energy_kwh"])


def generate_battery_and_soc(day, scenario, weather):
    low, high = SOC_BOUNDS[scenario]
    span = high - low
    weather_factor = {"cloudy": 0.34, "variable": 0.58, "clear": 0.78}[weather]
    amplitude = span * weather_factor * (0.42 if scenario != "hemat_plus" else 0.48)
    center = low + span * (0.50 if scenario != "hemat_plus" else 0.54)
    soc = []
    for index in range(96):
        phase = 2.0 * math.pi * (index / 96.0 - 0.25)
        value = center + amplitude * math.sin(phase)
        soc.append(min(high, max(low, value)))
    battery = []
    for index in range(96):
        next_soc = soc[(index + 1) % 96]
        # Positif = discharging (mengeluarkan daya), Negatif = charging
        battery.append(-((next_soc - soc[index]) / 100.0) * BATTERY_CAPACITY_KWH / INTERVAL_HOURS)
    return battery, soc


def build_dataset(output_csv="data/dispatch_dataset.csv"):
    """Membangun dataset CSV lengkap untuk pelatihan Machine Learning."""
    print("Memuat data target riset SmartGrid...")
    daily_data = load_research_targets()
    daily_load_targets = compute_daily_load_targets(daily_data)

    output_dir = Path(output_csv).parent
    output_dir.mkdir(parents=True, exist_ok=True)

    rows = []
    days = sorted(daily_data["baseline"].keys())

    print(f"Mengompilasi kurva dispatch 15-menit untuk {len(days)} hari x {len(SCENARIOS)} skenario...")

    for scenario in SCENARIOS:
        for day in days:
            target = daily_data[scenario][day]
            weather = get_weather(day, daily_data)
            load = generate_load_curve(day, daily_load_targets[day])
            pv = generate_pv_curve(day, scenario, target["pv_absorbed_kwh"], weather)
            genset = generate_genset_curve(target, pv, load)
            battery, soc = generate_battery_and_soc(day, scenario, weather)

            start_dt = datetime.fromisoformat(f"{day}T00:00:00")
            for i in range(96):
                dt = start_dt + timedelta(minutes=15 * i)
                hour = dt.hour + dt.minute / 60.0
                is_daytime = 1 if (6.0 <= hour <= 18.0) else 0

                load_val = round(load[i], 4)
                pv_val = round(pv[i], 4)
                net_load_val = round(load_val - pv_val, 4)
                soc_val = round(soc[i], 2)
                battery_val = round(battery[i], 4)
                genset_val = round(genset[i], 4)

                # Keputusan switching: 1 (Genset ON) jika genset menyuplai daya > 0.1 kW, 0 jika OFF
                genset_switch = 1 if genset_val > 0.1 else 0

                rows.append({
                    "timestamp": dt.isoformat(),
                    "date": day,
                    "scenario": scenario,
                    "weather": weather,
                    "hour": round(hour, 2),
                    "is_daytime": is_daytime,
                    "load_kw": load_val,
                    "pv_kw": pv_val,
                    "net_load_kw": net_load_val,
                    "soc_pct": soc_val,
                    "battery_kw": battery_val,
                    "genset_kw": genset_val,
                    "genset_switch": genset_switch
                })

    # Simpan ke CSV
    fieldnames = list(rows[0].keys())
    with open(output_csv, "w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)

    print(f" Dataset berhasil dibuat: {output_csv}")
    print(f"   Total baris data : {len(rows):,} baris")
    on_count = sum(1 for r in rows if r["genset_switch"] == 1)
    off_count = len(rows) - on_count
    print(f"   Distribusi Label : Genset ON={on_count:,} ({on_count/len(rows)*100:.1f}%), Genset OFF={off_count:,} ({off_count/len(rows)*100:.1f}%)")
    return output_csv


if __name__ == "__main__":
    build_dataset()
