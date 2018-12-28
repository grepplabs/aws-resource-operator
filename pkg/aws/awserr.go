package aws

import (
	"github.com/aws/aws-sdk-go/aws/awserr"
	"strings"
)

// Returns true if the error matches all these conditions:
//  * err is of type awserr.Error
//  * Error.Code() matches code
//  * Error.Message() contains message
func isAWSErr(err error, code string, message string) bool {
	if err, ok := err.(awserr.Error); ok {
		return err.Code() == code && strings.Contains(err.Message(), message)
	}
	return false
}

// IsAWSErrExtended returns true if the error matches all conditions
//  * err is of type awserr.Error
//  * Error.Code() matches code
//  * Error.Message() contains message
//  * Error.OrigErr() contains origErrMessage
// Note: This function will be moved out of the aws package in the future.
func IsAWSErrExtended(err error, code string, message string, origErrMessage string) bool {
	if !isAWSErr(err, code, message) {
		return false
	}
	return strings.Contains(err.(awserr.Error).OrigErr().Error(), origErrMessage)
}
