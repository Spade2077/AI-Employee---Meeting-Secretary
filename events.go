// 本文件负责构造状态变化和转写推送事件。
package main

func (s *Server) stateUpdate(meeting *Meeting, event *StateEvent) StateUpdate {
	update := StateUpdate{
		Type:      "state_update",
		Status:    string(StatusStandby),
		Meeting:   meeting,
		Event:     event,
		Timestamp: nowString(),
	}
	if meeting != nil {
		update.MeetingID = meeting.ID
		update.Status = meeting.Status
		update.PreviousStatus = meeting.PreviousStatus
		update.DurationSeconds = meeting.DurationSeconds
	}
	return update
}

func (s *Server) transcriptDelta(meetingID string, utterance *Utterance) TranscriptDelta {
	return TranscriptDelta{
		Type:      "transcript_delta",
		MeetingID: meetingID,
		Utterance: utterance,
		Timestamp: nowString(),
	}
}
