"""
Inference Engine untuk SmartGrid Energy Switching
Menggabungkan Model Machine Learning (Random Forest) dengan Lapisan Aturan Keselamatan Kelistrikan (Safety Guardrails).
"""

import os
from pathlib import Path
from datetime import datetime
from typing import Dict, Any, Optional
import numpy as np
import joblib


class SwitchingPredictor:
    """Mesin inferensi untuk menentukan keputusan switching On-Off genset vs PV & baterai."""

    def __init__(self, model_path: str = "models/switching_model.joblib"):
        self.model_path = Path(model_path)
        self.artifact = None
        self.model = None
        self.feature_names = None
        self._load_or_train()

    def _load_or_train(self):
        """Memuat model tersimpan, atau melatih secara otomatis jika belum ada."""
        if not self.model_path.exists():
            print(f"Model {self.model_path} tidak ditemukan. Melatih model baru secara otomatis...")
            from train import train_model
            self.artifact = train_model(model_output_path=str(self.model_path))
        else:
            self.artifact = joblib.load(self.model_path)

        self.model = self.artifact["model"]
        self.feature_names = self.artifact["feature_names"]

    def predict(
        self,
        load_kw: float,
        pv_kw: float,
        soc_pct: float,
        timestamp: Optional[str] = None,
        hour: Optional[float] = None
    ) -> Dict[str, Any]:
        """
        Menentukan rekomendasi switching genset.
        
        Args:
            load_kw: Beban listrik yang dibutuhkan konsumen (kW)
            pv_kw: Daya yang dihasilkan panel surya (kW)
            soc_pct: Persentase kapasitas baterai (10.0% - 100.0%)
            timestamp: Waktu pengukuran (format ISO string opsional)
            hour: Jam dalam desimal 0.0 - 23.9 (opsional, jika tidak ada dihitung dari waktu)
        """
        # 1. Parsing waktu dan fitur jam
        if hour is None:
            if timestamp:
                dt = datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
                hour = dt.hour + dt.minute / 60.0
            else:
                now = datetime.now()
                hour = now.hour + now.minute / 60.0

        is_daytime = 1 if (6.0 <= hour <= 18.0) else 0
        net_load_kw = round(load_kw - pv_kw, 4)

        # 2. LAPISAN ATURAN KESELAMATAN KELISTRIKAN (SAFETY GUARDRAILS)
        # Aturan 1: Proteksi Deep-Discharge Baterai (Hard-Critical Low SOC)
        if soc_pct <= 15.0:
            return {
                "genset_switch": 1,
                "action": "GENSET_ON",
                "power_source": "GENERATOR",
                "confidence": 1.0,
                "guardrail_triggered": True,
                "guardrail_rule": "CRITICAL_LOW_SOC_PROTECTION",
                "reason": f"Kapasitas baterai kritis ({soc_pct:.1f}% <= 15.0%). Genset WAJIB dinyalakan untuk mencegah blackout dan kerusakan sel baterai.",
                "input_parameters": {
                    "load_kw": load_kw,
                    "pv_kw": pv_kw,
                    "net_load_kw": net_load_kw,
                    "soc_pct": soc_pct,
                    "hour": round(hour, 2),
                    "is_daytime": is_daytime
                }
            }

        # Aturan 2: Proteksi Pemborosan Energi Surya (Excess Solar Generation)
        # Jika PV sendiri sudah mencukupi seluruh beban dan baterai masih di atas batas aman
        if pv_kw >= load_kw and soc_pct >= 25.0:
            return {
                "genset_switch": 0,
                "action": "GENSET_OFF",
                "power_source": "SOLAR_PV_AND_BATTERY",
                "confidence": 1.0,
                "guardrail_triggered": True,
                "guardrail_rule": "EXCESS_SOLAR_PROTECTION",
                "reason": f"Produksi panel surya ({pv_kw:.1f} kW) mencukupi seluruh beban ({load_kw:.1f} kW). Genset WAJIB dimatikan untuk efisiensi bahan bakar.",
                "input_parameters": {
                    "load_kw": load_kw,
                    "pv_kw": pv_kw,
                    "net_load_kw": net_load_kw,
                    "soc_pct": soc_pct,
                    "hour": round(hour, 2),
                    "is_daytime": is_daytime
                }
            }

        # Aturan 3: Baterai Hampir Penuh (High SOC)
        if soc_pct >= 90.0 and (pv_kw + 20.0 >= load_kw):
            return {
                "genset_switch": 0,
                "action": "GENSET_OFF",
                "power_source": "SOLAR_PV_AND_BATTERY",
                "confidence": 0.98,
                "guardrail_triggered": True,
                "guardrail_rule": "HIGH_SOC_SUPPLY",
                "reason": f"Kapasitas baterai sangat tinggi ({soc_pct:.1f}%) dan mampu menutup kebutuhan beban. Genset diposisikan OFF.",
                "input_parameters": {
                    "load_kw": load_kw,
                    "pv_kw": pv_kw,
                    "net_load_kw": net_load_kw,
                    "soc_pct": soc_pct,
                    "hour": round(hour, 2),
                    "is_daytime": is_daytime
                }
            }

        # 3. KEPUTUSAN MODEL MACHINE LEARNING (RANDOM FOREST)
        import pandas as pd
        feature_df = pd.DataFrame([{
            "load_kw": load_kw,
            "pv_kw": pv_kw,
            "net_load_kw": net_load_kw,
            "soc_pct": soc_pct,
            "hour": hour,
            "is_daytime": is_daytime
        }])[self.feature_names]

        prediction = int(self.model.predict(feature_df)[0])
        probabilities = self.model.predict_proba(feature_df)[0]
        confidence = float(probabilities[prediction])

        action = "GENSET_ON" if prediction == 1 else "GENSET_OFF"
        power_source = "GENERATOR" if prediction == 1 else "SOLAR_PV_AND_BATTERY"

        reason = (
            f"Model ML menyarankan Genset {'MENYALA' if prediction == 1 else 'MATI'} "
            f"(Confidence: {confidence * 100:.1f}%) berdasarkan optimalisasi pola historis smartgrid."
        )

        return {
            "genset_switch": prediction,
            "action": action,
            "power_source": power_source,
            "confidence": round(confidence, 4),
            "guardrail_triggered": False,
            "guardrail_rule": None,
            "reason": reason,
            "input_parameters": {
                "load_kw": load_kw,
                "pv_kw": pv_kw,
                "net_load_kw": net_load_kw,
                "soc_pct": soc_pct,
                "hour": round(hour, 2),
                "is_daytime": is_daytime
            }
        }
