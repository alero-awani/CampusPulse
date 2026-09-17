// Command lambda runs the CampusPulse API on AWS Lambda behind an API Gateway
// HTTP API (payload format 2.0).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"

	"campuspulse/internal/app"
)

func main() {
	a, err := app.Build(context.Background(), "api")
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup failed:", err)
		os.Exit(1)
	}
	lambda.Start(httpadapter.NewV2(a.Handler).ProxyWithContext)
}
