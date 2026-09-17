// Command escalator is a Lambda function, run on an EventBridge schedule, that
// escalates overdue service requests.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"

	"campuspulse/internal/app"
)

func main() {
	a, err := app.Build(context.Background(), "escalator")
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup failed:", err)
		os.Exit(1)
	}
	lambda.Start(func(ctx context.Context) (map[string]int, error) {
		n, err := a.Service.EscalateRequests(ctx)
		return map[string]int{"escalated": n}, err
	})
}
