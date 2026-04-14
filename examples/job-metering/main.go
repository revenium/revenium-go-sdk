package main

import (
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

	result, err := client.ReportJobOutcome("example-job-123", &jobs.JobOutcome{
		Status: "completed",
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
		fmt.Printf("  - %s (%s)\n", j.ID, j.Type)
	}
}
