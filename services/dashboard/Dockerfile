# Menggunakan python versi ringan
FROM python:3.9-slim

# Menentukan direktori kerja di dalam container
WORKDIR /app

# Salin requirements.txt dulu (agar layer cache pip efisien)
COPY requirements.txt .

# Install semua library dari requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

# Salin seluruh isi folder hmi ke dalam container
COPY . .

# Membuka port 5000
EXPOSE 5000

# Menjalankan aplikasi
CMD ["gunicorn", "--bind", "0.0.0.0:5000", "--workers", "2", "--timeout", "30", "app:app"]
