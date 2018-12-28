package aws

import (
	"fmt"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("aws")

func logInfof(format string, a ...interface{}) {
	log.Info(fmt.Sprintf(format, a...))
}
