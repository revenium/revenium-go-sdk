package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/revenium/revenium-go-sdk/core/jobs"
)

func main() {
	client, err := jobs.NewJobClient(jobs.JobClientConfig{
		APIKey: os.Getenv("REVENIUM_METERING_API_KEY"),
		TeamID: os.Getenv("REVENIUM_TEAM_ID"),
	})
	if err != nil {
		log.Fatal(err)
	}

	jobID := "example-job-456"

	_, err = client.ReportJobOutcome(jobID, &jobs.JobOutcome{
		ExecutionStatus: jobs.ExecutionStatusSuccess,
		OutcomeType:     jobs.OutcomeConverted,
	})
	if err != nil {
		var alreadyReported *jobs.OutcomeAlreadyReportedError
		if errors.As(err, &alreadyReported) {
			fmt.Printf("Outcome already reported for %s (amendments: %d)\n", alreadyReported.JobID, alreadyReported.AmendmentCount)
		} else {
			log.Fatal(err)
		}
	}

	val := 150.0
	amended, err := client.AmendJobOutcome(jobID, &jobs.JobOutcomeAmendment{
		Reason:          "correcting outcome value after review",
		ExecutionStatus: jobs.ExecutionStatusSuccess,
		OutcomeType:     jobs.OutcomeConverted,
		OutcomeValue:    &val,
		OutcomeCurrency: "USD",
	})
	if err != nil {
		var notReported *jobs.OutcomeNotReportedError
		var conflict *jobs.OutcomeAmendConflictError
		switch {
		case errors.As(err, &notReported):
			fmt.Printf("No outcome to amend for %s\n", notReported.JobID)
		case errors.As(err, &conflict):
			fmt.Printf("Concurrent amendment conflict for %s, refetch and retry\n", conflict.JobID)
		default:
			log.Fatal(err)
		}
		return
	}
	if amended.OutcomeAmendmentCount != nil {
		fmt.Printf("Outcome amended: %s (amendments: %d)\n", amended.ID, *amended.OutcomeAmendmentCount)
	} else {
		fmt.Printf("Outcome amended: %s\n", amended.ID)
	}

	history, err := client.GetJobOutcomeHistory(jobID)
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range history {
		reason := "(initial report)"
		if entry.Reason != nil {
			reason = *entry.Reason
		}
		fmt.Printf("  Seq %d: %s - %s\n", entry.AmendmentSequence, entry.ExecutionStatus, reason)
	}
}
