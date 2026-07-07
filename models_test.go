package main

import "testing"

func TestTranscriptPersistenceStates(t *testing.T) {
	if CanPersistTranscript(StatusStandby) {
		t.Fatal("standby audio should stay command-only")
	}
	if !CanPersistTranscript(StatusRecording) {
		t.Fatal("recording transcripts should persist")
	}
	if !CanPersistTranscript(StatusEnding) {
		t.Fatal("ending should persist the final in-flight transcript")
	}
	if CanPersistTranscript(StatusEnded) {
		t.Fatal("ended transcripts should not persist")
	}
}
