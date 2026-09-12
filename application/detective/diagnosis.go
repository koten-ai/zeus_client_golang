// SPDX-License-Identifier: BUSL-1.1

package detective

func gradeFromPrompt(verdict string) string {
	switch verdict {
	case "pass", "warn", "fail", "skip":
		return verdict
	default:
		return "n/a"
	}
}

func errorGrade(errN int) string {
	if errN <= 0 {
		return "pass"
	}
	if errN == 1 {
		return "warn"
	}
	return "fail"
}

func outputGrade(answer string, rows, errN int) string {
	if errN > 0 && answer == "" {
		return "fail"
	}
	if answer != "" && rows == 0 && errN == 0 {
		return "warn"
	}
	if answer != "" {
		return "pass"
	}
	return "warn"
}

func pipelineGrade(hops []map[string]any) string {
	var pipes []map[string]any
	for _, h := range hops {
		if hopName(h) == "pipeline" {
			pipes = append(pipes, h)
		}
	}
	if len(pipes) == 0 {
		return "n/a"
	}
	for _, h := range pipes {
		if hopFailed(h) {
			return "fail"
		}
	}
	return "pass"
}

func speedGrade(totalMS *int, rounds int) string {
	if totalMS == nil {
		return "n/a"
	}
	if *totalMS > 30000 || rounds > 6 {
		return "warn"
	}
	if *totalMS > 90000 {
		return "fail"
	}
	return "pass"
}

// DiagnosisArgs is Python build_diagnosis kwargs.
type DiagnosisArgs struct {
	Answer         string
	Hops           []map[string]any
	Notes          []string
	Prompt         map[string]any
	ContractStatus string
	LayerA         map[string]any
	TurnID         string
	ChatID         string
	SessionID      string
	Status         string
	TotalMS        *int
	Rounds         int
	Target         map[string]any
	ZeusURL        string
	ClientVersion  string
	Catalog        map[string]any
	Tokens         map[string]any
	ExportRef      string
}

// BuildDiagnosis is Python build_diagnosis.
func BuildDiagnosis(in DiagnosisArgs) map[string]any {
	prompt := in.Prompt
	if prompt == nil {
		prompt = map[string]any{}
	}
	errN := CountToolErrors(in.Hops, nil)
	rows := CountRowsSignal(in.Hops)
	promptGrade := gradeFromPrompt(asString(prompt["verdict"]))
	errGrade := errorGrade(errN)
	outGrade := outputGrade(in.Answer, rows, errN)
	pipeGrade := pipelineGrade(in.Hops)
	spdGrade := speedGrade(in.TotalMS, in.Rounds)

	playbooks := RunPlaybooks(in.Hops, in.Notes, in.Answer, prompt, in.ContractStatus, in.LayerA)

	headline := "Review prompt / tool grades"
	if len(playbooks) > 0 {
		headline = asString(playbooks[0]["summary"])
		if headline == "" {
			headline = asString(playbooks[0]["title"])
		}
		if headline == "" {
			headline = "Issues detected"
		}
	} else if promptGrade == "pass" && errGrade == "pass" {
		headline = "Turn looks healthy"
	}

	pref := PreferredReqID(in.Hops)
	reqIDs := CollectReqIDs(in.Hops)
	support := BuildSupportPack(SupportArgs{
		Headline:       headline,
		TurnID:         in.TurnID,
		ChatID:         in.ChatID,
		SessionID:      in.SessionID,
		PreferredReqID: pref,
		ReqIDs:         reqIDs,
		Hops:           in.Hops,
		Playbooks:      playbooks,
		PromptVerdict:  promptGrade,
		Notes:          in.Notes,
		Status:         in.Status,
		Target:         in.Target,
		ZeusURL:        in.ZeusURL,
		ClientVersion:  in.ClientVersion,
		Catalog:        in.Catalog,
		ContractStatus: in.ContractStatus,
		Inject:         asMap(prompt["inject"]),
		LayerA:         in.LayerA,
		Tokens:         in.Tokens,
		ExportRef:      in.ExportRef,
	})

	var total any
	if in.TotalMS != nil {
		total = *in.TotalMS
	}
	return map[string]any{
		"headline":       headline,
		"prompt_grade":   promptGrade,
		"speed_grade":    spdGrade,
		"error_grade":    errGrade,
		"output_grade":   outGrade,
		"pipeline_grade": pipeGrade,
		"prompt": map[string]any{
			"verdict": prompt["verdict"],
			"summary": prompt["summary"],
		},
		"slow":         map[string]any{"total_ms": total, "rounds": in.Rounds, "grade": spdGrade},
		"errors":       map[string]any{"count": errN, "grade": errGrade},
		"output":       map[string]any{"rows_signal": rows, "answer_chars": len(in.Answer), "grade": outGrade},
		"pipeline":     map[string]any{"grade": pipeGrade},
		"playbooks":    playbooks,
		"support_pack": support,
	}
}
