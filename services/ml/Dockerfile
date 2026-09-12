FROM python:3.11-slim

WORKDIR /app

# Install dependencies sistem jika diperlukan
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Salin requirements dan install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Salin seluruh kode aplikasi
COPY . .

# Verifikasi kesiapan model saat build image
RUN python3 -c "from predictor import SwitchingPredictor; p = SwitchingPredictor(); print('Model loaded successfully')"

EXPOSE 8000

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
