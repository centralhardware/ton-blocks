FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    TON_HEARTBEAT_FILE=/tmp/ton-blocks.heartbeat

WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY ton_blocks.py op_names.tsv op_names_registry.tsv ./
COPY migrations ./migrations

RUN useradd --system --no-create-home ton
USER ton

# unhealthy when no masterchain block was processed for 3 minutes
HEALTHCHECK --interval=30s --timeout=5s --start-period=120s --retries=3 \
  CMD python -c "import os,sys,time; f=os.environ['TON_HEARTBEAT_FILE']; sys.exit(0 if os.path.exists(f) and time.time()-os.path.getmtime(f) < 180 else 1)"

ENTRYPOINT ["python", "ton_blocks.py"]
