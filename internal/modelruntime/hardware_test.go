package modelruntime

import "testing"

func TestNvidiaDevices(t *testing.T) {
	got := nvidiaDevices("GPU One, 24576, 12000\nGPU Two, 8192, 7000\n")
	if len(got) != 2 || got[0].Total != 24576<<20 || got[1].Free != 7000<<20 {
		t.Fatalf("incorrect per-device inventory: %+v", got)
	}
	if len(nvidiaDevices("GPU, [N/A], [N/A]\n")) != 0 {
		t.Fatal("unknown memory must not be treated as usable capacity")
	}
}
