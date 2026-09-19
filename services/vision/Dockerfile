FROM python:3.11-slim-bookworm
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends build-essential libglib2.0-0 libgl1 && rm -rf /var/lib/apt/lists/*
COPY requirements.lock .
# CPU never published 1.20.2 (that tag is onnxruntime-gpu only). Last 1.20.x CPU is 1.20.1.
RUN pip install --no-cache-dir -r requirements.lock onnxruntime==1.20.1
COPY . .
ENV VISION_MODEL_DIR=/models/vision
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "3003", "--workers", "1"]
