package protocol

import "testing"

func TestParseHaptic(t *testing.T) {
	good := []string{
		`{"t":"haptic","pattern":[30,20,30]}`,
		`{"t":"haptic","name":"success"}`,
		`{"t":"haptic","name":"hit","intensity":0.4}`,
		`{"t":"haptic","pattern":[1000,1000,1000,1000]}`,
	}
	for _, g := range good {
		if _, err := ParseControl([]byte(g)); err != nil {
			t.Errorf("%s: %v", g, err)
		}
	}
	for _, n := range HapticNames {
		if _, err := ParseControl([]byte(`{"t":"haptic","name":"` + n + `"}`)); err != nil {
			t.Errorf("name %s: %v", n, err)
		}
	}
	bad := []string{
		`{"t":"haptic"}`,
		`{"t":"haptic","pattern":[]}`,
		`{"t":"haptic","pattern":"30"}`,
		`{"t":"haptic","name":"zap"}`,
		`{"t":"haptic","name":"tap","pattern":[10]}`,
		`{"t":"haptic","pattern":[1001]}`,
		`{"t":"haptic","pattern":[-1]}`,
		`{"t":"haptic","pattern":[1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1]}`,
		`{"t":"haptic","pattern":[1000,1000,1000,1000,1]}`,
		`{"t":"haptic","name":"tap","intensity":2}`,
	}
	for _, b := range bad {
		if _, err := ParseControl([]byte(b)); err == nil {
			t.Errorf("%s should be rejected", b)
		}
	}
}
