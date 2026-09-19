"""CUDA canary for the private vision runtime. Never downloads weights."""
import os

os.environ.setdefault("VISION_DEVICE", "cuda")

from model_canary import main

if __name__ == "__main__":
    main()
