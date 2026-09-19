# Model library and hardware

Administrators can use Settings → Models & ML → Model library to install weights,
inspect the ML worker's hardware, test models and assign installed models to tasks.
Assistant assignment requires Ollama's tools capability; context assignment requires
completion. Whisper presets can be installed and assigned without typing a model name.
Assignments affect subsequent jobs. Existing runs retain their configuration.

Telemetry is collected inside the ML container, not the browser: logical CPU count,
Linux memory availability capped by container limits, each NVIDIA GPU reported by
nvidia-smi, and AMD/Intel DRM devices exposed through sysfs. Unknown or hidden device
memory is not treated as usable capacity. Detected hardware alone does not establish
runtime compatibility. Multi-GPU memory is deliberately not summed into a fit promise.

The picker compares installed weight sizes against currently free memory only as a
lower bound. Context caches, concurrent jobs and backend overhead need additional
space. Loaded Ollama models show actual resident memory and GPU allocation. With a
remote OLLAMA_HOST, local worker memory is not used to predict model fit.

CPU transcription is supported: whisper.device=cpu passes -ng to whisper.cpp. Model
tests use the same selected device. The executable must still be runnable: selecting
CPU cannot fix a CUDA-linked binary missing shared libraries. Deploy the existing
runtime-cpu image target on CPU-only hosts, and use runtime-cuda or runtime-rocm with
appropriate drivers/device access for GPU hosts. Image/driver changes remain deployment
configuration, not live settings. This change does not automatically install weights,
change assignments, or change GPU passthrough.

The model-operation stream refreshes inventory and progress every five seconds. Actions
report status in place. Assigned or active weights cannot be removed by the runtime.
