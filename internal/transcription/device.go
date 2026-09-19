package transcription

// CPUPromptVersion marks ingest/catchup transcription that must not take the GPU.
// Interactive MCP / caption-regen jobs keep the empty or asr-v1 prompt and may use CUDA.
const CPUPromptVersion = "asr-cpu"
