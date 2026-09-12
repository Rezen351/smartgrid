"""
Training Pipeline untuk SmartGrid Energy Switching Classifier
Melatih model Random Forest untuk menentukan keputusan On-Off generator vs Baterai & PV.
"""

import os
import json
from pathlib import Path
from datetime import datetime
import pandas as pd
import numpy as np
from sklearn.model_selection import train_test_split
from sklearn.ensemble import RandomForestClassifier
from sklearn.metrics import accuracy_score, precision_score, recall_score, f1_score, classification_report, confusion_matrix
import joblib


FEATURE_COLS = ["load_kw", "pv_kw", "net_load_kw", "soc_pct", "hour", "is_daytime"]
TARGET_COL = "genset_switch"


def train_model(
    data_path: str = "data/dispatch_dataset.csv",
    model_output_path: str = "models/switching_model.joblib"
):
    print("=" * 60)
    print("🔋 PELATIHAN MODEL MACHINE LEARNING: SMART ENERGY SWITCHING 🔋")
    print("=" * 60)

    # 1. Pastikan dataset ada, jika belum ada buat otomatis
    if not os.path.exists(data_path):
        print(f"Dataset {data_path} belum ada. Menjalankan data_generator...")
        from data_generator import build_dataset
        build_dataset(data_path)

    print(f"Memuat dataset dari {data_path}...")
    df = pd.read_csv(data_path)
    print(f"Total baris data : {len(df):,} baris")

    X = df[FEATURE_COLS]
    y = df[TARGET_COL]

    # 2. Train-Test Split (80% Train, 20% Test)
    X_train, X_test, y_train, y_test = train_test_split(
        X, y, test_size=0.2, random_state=42, stratify=y
    )
    print(f"Data latih (train) : {len(X_train):,} sampel")
    print(f"Data uji (test)    : {len(X_test):,} sampel")

    # 3. Inisialisasi dan Latih Model Random Forest
    print("\nMelatih Random Forest Classifier...")
    model = RandomForestClassifier(
        n_estimators=120,
        max_depth=12,
        min_samples_split=4,
        random_state=42,
        class_weight="balanced",
        n_jobs=-1
    )
    model.fit(X_train, y_train)

    # 4. Evaluasi Model pada Data Uji
    y_pred = model.predict(X_test)
    acc = accuracy_score(y_test, y_pred)
    prec = precision_score(y_test, y_pred)
    rec = recall_score(y_test, y_pred)
    f1 = f1_score(y_test, y_pred)
    cm = confusion_matrix(y_test, y_pred)

    print("\n" + "=" * 40)
    print("📊 HASIL EVALUASI MODEL (DATA UJI):")
    print("=" * 40)
    print(f"Akurasi    : {acc * 100:.2f}%")
    print(f"Precision  : {prec * 100:.2f}%")
    print(f"Recall     : {rec * 100:.2f}%")
    print(f"F1-Score   : {f1 * 100:.2f}%")
    print(f"\nConfusion Matrix:\n{cm}")
    print("\nClassification Report:\n", classification_report(y_test, y_pred, target_names=["Genset OFF (PV+Baterai)", "Genset ON"]))

    # 5. Analisis Tingkat Kepentingan Fitur (Feature Importance)
    print("\n📈 Tingkat Kepentingan Fitur (Feature Importance):")
    importances = model.feature_importances_
    sorted_indices = np.argsort(importances)[::-1]
    for idx in sorted_indices:
        print(f"  - {FEATURE_COLS[idx]:15s} : {importances[idx] * 100:.2f}%")

    # 6. Simpan Model dan Artefak
    out_dir = Path(model_output_path).parent
    out_dir.mkdir(parents=True, exist_ok=True)

    artifact = {
        "model": model,
        "feature_names": FEATURE_COLS,
        "metrics": {
            "accuracy": float(acc),
            "precision": float(prec),
            "recall": float(rec),
            "f1_score": float(f1)
        },
        "trained_at": datetime.now().isoformat()
    }
    joblib.dump(artifact, model_output_path)
    print(f"\n Model berhasil disimpan ke: {model_output_path}")
    return artifact


if __name__ == "__main__":
    train_model()
