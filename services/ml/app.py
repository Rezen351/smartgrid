"""
FastAPI Microservice untuk SmartGrid Energy Switching Machine Learning
Menyediakan REST API & Visual Interactive Simulator untuk inferensi keputusan
switching On-Off generator vs panel surya & baterai.
"""

from typing import Any, Dict, List, Literal, Optional
from pathlib import Path
from fastapi import FastAPI, HTTPException, Response
from fastapi.responses import HTMLResponse
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field

from predictor import SwitchingPredictor
from api_client import SmartGridClient

description_md = """
### ⚡ SmartGrid AI Energy Switching Service

Layanan kecerdasan buatan berbasis **Random Forest (Akurasi 97.7%)** dan **Safety Guardrails** untuk menentukan kapan menyalakan generator (Genset) vs kapan memanfaatkan energi terbarukan (Solar PV & Baterai).

👉 **[Buka Simulator Visual Interaktif (Klik Disini)](/)** untuk mencoba simulasi dengan tampilan visual yang ramah bagi orang awam.

---
"""

app = FastAPI(
    title="SmartGrid AI Energy Switching Controller",
    description=description_md,
    version="1.0.0",
    docs_url="/docs",
    redoc_url="/redoc",
    openapi_url="/openapi.json",
    openapi_tags=[
        {"name": "Sistem", "description": "Endpoint status dan metadata service."},
        {"name": "Machine Learning", "description": "Inferensi keputusan switching genset vs PV & baterai."},
        {"name": "Web Visual Dashboard", "description": "Antarmuka visual interaktif simulator."},
    ],
)

# Aktifkan CORS agar frontend dapat memanggil API tanpa kendala
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Inisialisasi predictor saat startup
predictor = SwitchingPredictor()
TEMPLATES_DIR = Path(__file__).resolve().parent / "templates"


class PredictRequest(BaseModel):
    load_kw: float = Field(..., description="Kebutuhan beban listrik pengguna dalam kW (contoh: 35.5)")
    pv_kw: float = Field(0.0, description="Daya yang dihasilkan panel surya dalam kW (contoh: 20.0)")
    soc_pct: float = Field(..., description="Level kapasitas baterai saat ini dalam % (contoh: 65.0)")
    timestamp: Optional[str] = Field(None, description="Waktu ISO8601 opsional (contoh: '2026-09-12T14:30:00Z')")
    hour: Optional[float] = Field(None, description="Jam opsional dalam desimal 0.0 - 23.9 (contoh: 14.5)")


class SyncPredictRequest(BaseModel):
    node_id: str = Field("SmartGrid-01", description="ID node telemetri dari server SmartGrid")
    fallback_pv_kw: float = Field(0.0, description="Nilai estimasi PV jika belum dipasang di sensor (kW)")
    fallback_soc_pct: float = Field(50.0, description="Nilai estimasi SOC jika belum dipasang di sensor (%)")


class InputParametersResponse(BaseModel):
    load_kw: float
    pv_kw: float
    net_load_kw: float
    soc_pct: float
    hour: float
    is_daytime: int


class PredictionResult(BaseModel):
    genset_switch: Literal[0, 1]
    action: Literal["GENSET_ON", "GENSET_OFF"]
    power_source: Literal["GENERATOR", "SOLAR_PV_AND_BATTERY"]
    confidence: float
    guardrail_triggered: bool
    guardrail_rule: Optional[str]
    reason: str
    input_parameters: InputParametersResponse


class PredictResponse(BaseModel):
    success: bool
    data: PredictionResult


class SyncPredictResponse(BaseModel):
    success: bool
    source_telemetry: Dict[str, Any]
    data: PredictionResult


class ApiInfoResponse(BaseModel):
    service: str
    version: str
    status: str
    docs_url: str
    model_metrics: Dict[str, Any]


class HealthResponse(BaseModel):
    status: str
    model_loaded: bool
    features: List[str]


OPENAPI_FILE = Path(__file__).resolve().parent / "openapi.yaml"


@app.get("/", response_class=HTMLResponse, tags=["Web Visual Dashboard"])
def visual_dashboard():
    """
    Antarmuka Web Visual Interaktif dengan slider kendali listrik,
    indikator status genset, dan visualisasi yang mudah dipahami orang awam.
    """
    html_file = TEMPLATES_DIR / "index.html"
    if html_file.exists():
        return HTMLResponse(content=html_file.read_text(encoding="utf-8"))
    return HTMLResponse(content="<h1>Dashboard file not found</h1>", status_code=404)


@app.get("/favicon.ico", include_in_schema=False)
def favicon():
    svg = """<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#38bdf8"><path d="M11 21h-1l1-7H7.5c-.58 0-.57-.32-.38-.66.19-.34.05-.08.08-.14C8.28 11.23 10.42 7.5 13.5 2h1l-1 7h3.5c.49 0 .56.33.47.51l-.07.15C14.7 14.88 12.5 19 11 21z"/></svg>"""
    return Response(content=svg, media_type="image/svg+xml")


@app.get("/api/info", response_model=ApiInfoResponse, tags=["Sistem"])
def api_info() -> ApiInfoResponse:
    """Metadata teknis tentang versi model dan metrik akurasi."""
    return {
        "service": "SmartGrid Energy Switching ML Microservice",
        "version": "1.0.0",
        "status": "ready",
        "docs_url": "/docs",
        "model_metrics": predictor.artifact.get("metrics", {}) if predictor.artifact else {}
    }


@app.get("/health", response_model=HealthResponse, tags=["Sistem"])
def health_check() -> HealthResponse:
    """Status kesehatan container dan model."""
    return {
        "status": "ok",
        "model_loaded": predictor.model is not None,
        "features": predictor.feature_names
    }


@app.post("/predict-switch", response_model=PredictResponse, tags=["Machine Learning"])
def predict_switch(req: PredictRequest) -> PredictResponse:
    """
    Menghitung rekomendasi switching On-Off generator vs baterai & PV berdasarkan parameter listrik.
    """
    try:
        result = predictor.predict(
            load_kw=req.load_kw,
            pv_kw=req.pv_kw,
            soc_pct=req.soc_pct,
            timestamp=req.timestamp,
            hour=req.hour
        )
        return {
            "success": True,
            "data": result
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/sync-and-predict", response_model=SyncPredictResponse, tags=["Machine Learning"])
def sync_and_predict(req: SyncPredictRequest) -> SyncPredictResponse:
    """
    Menarik data telemetri live dari server SmartGrid (Export API Almuzky)
    lalu secara otomatis menghitung rekomendasi switching.
    """
    try:
        client = SmartGridClient()
        telemetry = client.get_telemetry(node_id=req.node_id, limit=1, output_format="json")
        rows = telemetry.get("data", {}).get("rows", [])

        if not rows:
            raise HTTPException(status_code=404, detail=f"Tidak ada data telemetri ditemukan untuk node {req.node_id}")

        latest = rows[0]
        sensor_val = float(latest.get("value", 30.0))
        measured_time = latest.get("time")

        load_kw = sensor_val

        result = predictor.predict(
            load_kw=load_kw,
            pv_kw=req.fallback_pv_kw,
            soc_pct=req.fallback_soc_pct,
            timestamp=measured_time
        )

        return {
            "success": True,
            "source_telemetry": latest,
            "data": result
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Gagal sinkronisasi telemetri: {str(e)}")


def _read_openapi_spec() -> Response:
    if not OPENAPI_FILE.exists():
        raise HTTPException(status_code=404, detail="OpenAPI specification not found")
    return Response(content=OPENAPI_FILE.read_text(encoding="utf-8"), media_type="application/yaml")


@app.get("/openapi.yaml", include_in_schema=False)
def openapi_yaml_spec() -> Response:
    """Alias untuk spesifikasi OpenAPI dalam format YAML."""
    return _read_openapi_spec()


@app.get(
    "/openapi",
    include_in_schema=False,
    responses={
        200: {
            "description": "Spesifikasi OpenAPI untuk ML Service dalam format YAML",
            "content": {"application/yaml": {"schema": {"type": "string"}}},
        },
        404: {"description": "Spesifikasi OpenAPI tidak ditemukan"},
    },
)
def openapi_spec() -> Response:
    """Spesifikasi OpenAPI statis untuk ML Service."""
    return _read_openapi_spec()


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
