package aws

import (
	"github.com/aws/aws-sdk-go/aws/awserr"
	"strings"
)

// Returns true if the error matches all these conditions:
//  * err is of type awserr.Error
//  * Error.Code() matches code
//  * Error.Message() contains message
func IsAWSErr(err error, code string, message string) bool {
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
	if !IsAWSErr(err, code, message) {
		return false
	}
	return strings.Contains(err.(awserr.Error).OrigErr().Error(), origErrMessage)
}

// Returns true if the error matches all these conditions:
//  * err is of type awserr.Error
//  * Error.Code() matches one of codes
func IsAWSCode(err error, codes []string) bool {
	if err != nil {
		awsErr, ok := err.(awserr.Error)
		if ok {
			for _, code := range codes {
				if awsErr.Code() == code {
					return true
				}
			}
		}
	}
	return false
}

type RetryError struct {
	Err       error
	Retryable bool
	Reason    string
}

// RetryableError is a helper to create a RetryError that's retryable from a
// given error.
func RetryableError(err error, reason string) *RetryError {
	if err == nil {
		return nil
	}
	return &RetryError{Err: err, Retryable: true, Reason: reason}
}

// NonRetryableError is a helper to create a RetryError that's _not_ retryable
// from a given error.
func NonRetryableError(err error, reason string) *RetryError {
	if err == nil {
		return nil
	}
	return &RetryError{Err: err, Retryable: false, Reason: reason}
}
