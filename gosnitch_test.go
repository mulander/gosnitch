package gosnitch

import "testing"

func TestPidof(t *testing.T) {
	_, err := Pidof("go")
	if err != nil {
		t.Error("Did not find pid of 'go'")
	}
}

func TestPidofNotExisting(t *testing.T) {
	pid, err := Pidof("fake-process")
	if err == nil {
		t.Errorf("Found pid %s when expected to find nothing", pid)
	}
}

func TestProbe(t *testing.T) {
	pid, err := Pidof("go")
	if err != nil {
		t.Error("Expected to find 'go' pid but found nothing")
	}

	s := NewTopSampler(pid)
	s.Probe(pid)
}