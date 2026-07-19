package drift

import "github.com/thefynx/reeve/internal/notify"

// NotifyPayloads flattens a RunOutput into notification payloads, one per
// item with a non-silent event. This is the drift producer's adapter onto
// the shared sink framework.
func NotifyPayloads(out *RunOutput) []notify.Payload {
	if out == nil {
		return nil
	}
	payloads := make([]notify.Payload, 0, len(out.Items))
	for i, it := range out.Items {
		ev := out.Events[i]
		if ev == EventNone {
			continue
		}
		payloads = append(payloads, notify.Payload{
			Event: notify.Event(ev),
			Drift: &notify.DriftPayload{
				Project:     it.Project,
				Stack:       it.Stack,
				Env:         it.Env,
				Outcome:     string(it.Outcome),
				Add:         it.Counts.Counts.Add,
				Change:      it.Counts.Counts.Change,
				Delete:      it.Counts.Counts.Delete,
				Replace:     it.Counts.Counts.Replace,
				PlanSummary: it.Counts.PlanSummary,
				Fingerprint: it.Fingerprint,
				Error:       it.Error,
				RunID:       out.RunID,
			},
		})
	}
	return payloads
}
