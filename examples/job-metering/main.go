package main

import (
	"fmt"
	"log"
	"os"

	"github.com/revenium/revenium-go-sdk/core/jobs"
)

func main() {
	writeKey := os.Getenv("REVENIUM_WRITE_API_KEY")
	if writeKey == "" {
		log.Fatal("set REVENIUM_WRITE_API_KEY to a write key (rev_sk_): job and outcome calls refuse the metering key (rev_mk_)")
	}

	client, err := jobs.NewJobClient(jobs.JobClientConfig{
		APIKey: writeKey,
		TeamID: os.Getenv("REVENIUM_TEAM_ID"),
	})
	if err != nil {
		log.Fatal(err)
	}

	result, err := client.ReportJobOutcome("example-job-123", &jobs.JobOutcome{
		ExecutionStatus: jobs.ExecutionStatusSuccess,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Job outcome reported: %s\n", result.ID)

	pagedJobs, err := client.ListJobs(&jobs.ListJobsParams{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Total jobs: %d\n", len(pagedJobs.Content))
	for _, j := range pagedJobs.Content {
		jobType := ""
		if j.Type != nil {
			jobType = *j.Type
		}
		fmt.Printf("  - %s (%s)\n", j.ID, jobType)
	}
}
