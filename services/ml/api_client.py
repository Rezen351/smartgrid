"""
SmartGrid API Client for Machine Learning
Menyediakan interface sederhana untuk autentikasi dan penarikan data telemetri
dari SmartGrid Export API (TimescaleDB).
"""

import os
import io
import time
import requests
from typing import Dict, List, Optional, Any, Union


class SmartGridClient:
    """Client untuk berinteraksi dengan API SmartGrid."""

    def __init__(
        self,
        base_url: Optional[str] = None,
        username: Optional[str] = None,
        password: Optional[str] = None,
        env_file: str = ".env"
    ):
        # Muat variabel dari .env jika ada
        self._load_env(env_file)

        self.base_url = (base_url or os.getenv("SMARTGRID_API_URL") or "https://smartgrid.almuzky.my.id/api/v1").rstrip("/")
        self.username = username or os.getenv("SMARTGRID_USERNAME")
        self.password = password or os.getenv("SMARTGRID_PASSWORD")

        self.access_token: Optional[str] = None
        self.refresh_token: Optional[str] = None
        self.token_expiry: float = 0

    def _load_env(self, env_path: str):
        """Helper membaca file .env tanpa perlu dependensi pihak ketiga."""
        if not os.path.exists(env_path):
            return
        with open(env_path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                k, v = line.split("=", 1)
                os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))

    def login(self) -> bool:
        """Melakukan autentikasi ke Auth Service untuk mendapatkan token JWT."""
        if not self.username or not self.password:
            raise ValueError("Username dan password harus disediakan.")

        url = f"{self.base_url}/auth/login"
        payload = {
            "identifier": self.username,
            "password": self.password
        }

        resp = requests.post(url, json=payload, timeout=10)
        if resp.status_code != 200:
            raise RuntimeError(f"Login gagal (HTTP {resp.status_code}): {resp.text}")

        data = resp.json()
        if not data.get("success"):
            raise RuntimeError(f"Login ditolak: {data}")

        auth_data = data.get("data", {})
        self.access_token = auth_data.get("access_token")
        self.refresh_token = auth_data.get("refresh_token")
        # Token berlaku 15 menit (900 detik), simpan waktu kadaluarsa dengan buffer 30 detik
        expires_in = auth_data.get("expires_in", 900)
        self.token_expiry = time.time() + expires_in - 30
        return True

    def _get_valid_token(self) -> str:
        """Memastikan token masih valid, jika kadaluarsa akan login ulang."""
        if not self.access_token or time.time() >= self.token_expiry:
            self.login()
        return self.access_token

    def _headers(self) -> Dict[str, str]:
        token = self._get_valid_token()
        return {
            "Authorization": f"Bearer {token}",
            "Accept": "application/json"
        }

    def check_health(self) -> Dict[str, Any]:
        """Cek status kesehatan Export API (tidak membutuhkan token)."""
        url = f"{self.base_url}/export/health"
        resp = requests.get(url, timeout=10)
        return resp.json()

    def get_nodes(self) -> List[Dict[str, Any]]:
        """Mendapatkan daftar node dan metrik sensor yang tersedia."""
        url = f"{self.base_url}/export/nodes"
        resp = requests.get(url, headers=self._headers(), timeout=10)
        if resp.status_code != 200:
            raise RuntimeError(f"Gagal mengambil nodes (HTTP {resp.status_code}): {resp.text}")
        data = resp.json()
        return data.get("data", {}).get("nodes", [])

    def get_meta(
        self,
        node_id: str = "*",
        metric: str = "*",
        from_time: Optional[str] = None,
        to_time: Optional[str] = None
    ) -> Dict[str, Any]:
        """Melihat perkiraan jumlah baris data telemetri yang cocok."""
        url = f"{self.base_url}/export/meta"
        params = {"node_id": node_id, "metric": metric}
        if from_time:
            params["from"] = from_time
        if to_time:
            params["to"] = to_time

        resp = requests.get(url, headers=self._headers(), params=params, timeout=10)
        if resp.status_code != 200:
            raise RuntimeError(f"Gagal mengambil metadata (HTTP {resp.status_code}): {resp.text}")
        return resp.json().get("data", {})

    def get_telemetry(
        self,
        node_id: str = "*",
        metric: str = "*",
        from_time: Optional[str] = None,
        to_time: Optional[str] = None,
        limit: int = 1000,
        output_format: str = "json",
        cursor: Optional[str] = None
    ) -> Union[Dict[str, Any], str]:
        """
        Mengambil data telemetri.
        
        Args:
            node_id: ID node (misal 'SmartGrid-01' atau '*')
            metric: Nama sensor/metrik (misal 'reg504' atau '*')
            from_time: Awal rentang waktu (format RFC3339 misal '2026-09-12T00:00:00Z')
            to_time: Akhir rentang waktu
            limit: Jumlah baris data per request (default 1000, maks 100000)
            output_format: 'json' atau 'csv'
            cursor: Token halaman selanjutnya jika has_more bernilai True
        """
        url = f"{self.base_url}/export/telemetry"
        params = {
            "node_id": node_id,
            "metric": metric,
            "limit": limit,
            "format": output_format
        }
        if from_time:
            params["from"] = from_time
        if to_time:
            params["to"] = to_time
        if cursor:
            params["cursor"] = cursor

        headers = self._headers()
        if output_format == "csv":
            headers["Accept"] = "text/csv"

        resp = requests.get(url, headers=headers, params=params, timeout=30)
        if resp.status_code != 200:
            raise RuntimeError(f"Gagal mengambil telemetry (HTTP {resp.status_code}): {resp.text}")

        if output_format == "csv":
            return resp.text
        return resp.json()

    def get_telemetry_df(
        self,
        node_id: str = "*",
        metric: str = "*",
        from_time: Optional[str] = None,
        to_time: Optional[str] = None,
        limit: int = 10000
    ):
        """
        Helper praktis: Mengambil data dan langsung mengonversinya menjadi Pandas DataFrame.
        Catatan: Membutuhkan package 'pandas' terinstall.
        """
        try:
            import pandas as pd
        except ImportError:
            raise ImportError("Package 'pandas' belum terinstall. Jalankan: pip install pandas")

        csv_text = self.get_telemetry(
            node_id=node_id,
            metric=metric,
            from_time=from_time,
            to_time=to_time,
            limit=limit,
            output_format="csv"
        )
        df = pd.read_csv(io.StringIO(csv_text))
        if "time" in df.columns:
            df["time"] = pd.to_datetime(df["time"])
        return df
