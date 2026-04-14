package main

import (
	"fmt"
	"os"

	"github.com/revenium/revenium-go-sdk/core/jobs"
)

func main() {
	client, err := jobs.NewJobClient(jobs.JobClientConfig{
		APIKey: os.Getenv("REVENIUM_METERING_API_KEY"),
		TeamID: os.Getenv("REVENIUM_TEAM_ID"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create job client: %v\n", err)
		os.Exit(1)
	}

	result, err := client.ReportJobOutcome("example-job-123", &jobs.JobOutcome{
		Status: "completed",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to report job outcome: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Job outcome reported: %s\n", result.ID)

	pagedJobs, err := client.ListJobs(&jobs.ListJobsParams{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list jobs: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Total jobs: %d\n", len(pagedJobs.Content))
}
