/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package s3bucket

import (
	"errors"
	"fmt"
	"golang.org/x/net/context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"k8s.io/client-go/util/flowcontrol"

	awsv1beta1 "github.com/grepplabs/aws-resource-operator/pkg/apis/aws/v1beta1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const timeout = time.Second * 5

type mockS3API struct {
	s3iface.S3API
}

func (m mockS3API) HeadBucket(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
	return nil, fmt.Errorf("TODO: Implement testing")
}

func newTestReconciler(mgr manager.Manager, s3conn s3iface.S3API) reconcile.Reconciler {
	return &ReconcileS3Bucket{
		Client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		recorder:  mgr.GetRecorder("s3bucket-controller"),
		partition: "aws",
		region:    "eu-central-1",
		s3conn:    s3conn,
	}
}

var _ = Describe("S3Bucket Reconcile Suite", func() {
	var (
		c               client.Client
		requests        chan reconcile.Request
		stopMgr         chan struct{}
		mgrStopped      *sync.WaitGroup
		expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "s3-foo-bucket", Namespace: "default"}}
	)
	BeforeEach(func() {
		cfg.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		mgr, err := manager.New(cfg, manager.Options{})
		Expect(err).NotTo(HaveOccurred())

		c = mgr.GetClient()

		mockS3API := mockS3API{}
		var recFn reconcile.Reconciler
		recFn, requests = SetupTestReconcile(newTestReconciler(mgr, mockS3API))
		Expect(add(mgr, recFn)).NotTo(HaveOccurred())

		stopMgr, mgrStopped = StartTestManager(mgr)

	})

	Context("Create S3Bucket", func() {
		It("First try", func() {
			instance := &awsv1beta1.S3Bucket{ObjectMeta: metav1.ObjectMeta{Name: "s3-foo-bucket", Namespace: "default"}}
			err := c.Create(context.TODO(), instance)
			Expect(err).NotTo(HaveOccurred())
			defer c.Delete(context.TODO(), instance)
			Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
		})
	})

	AfterEach(func() {
		close(stopMgr)
		mgrStopped.Wait()
	})
})

var _ = Describe("S3Bucket Help Functions Suite", func() {
	DescribeTable("Se event message from error",
		func(input string, expected string) {
			Expect(eventMessageFromError(errors.New(input))).To(Equal(expected))
		},
		Entry("empty input",
			"",
			"Reconcile failed",
		),
		Entry("multi-line s3api error message",
			"error reading S3 Bucket (bar): BucketRegionError: incorrect region, the bucket is not in 'eu-central-1' region\n\tstatus code: 301, request id: , host id: ",
			"error reading S3 Bucket (bar): BucketRegionError: incorrect region, the bucket is not in 'eu-central-1' region",
		),
		Entry("s3api error message with newline escape sequence",
			`error reading S3 Bucket (bar): BucketRegionError: incorrect region, the bucket is not in 'eu-central-1' region\n\tstatus code: 301, request id: , host id: `,
			"error reading S3 Bucket (bar): BucketRegionError: incorrect region, the bucket is not in 'eu-central-1' region",
		),
		Entry("s3api error message with extra params",
			"error reading S3 Bucket (s3://bar): BadRequest: Bad Request\n\tstatus code: 400, request id: 02F83228BF1D6082, host id: N4qlRz2PYekN1kk5kY5DvjiCiV46lKxpyWXNQ10MZl3InmKtjXeMyuyvlKeOEqc/F1EOQpIg+hQ=",
			"error reading S3 Bucket (s3://bar): BadRequest: Bad Request",
		),
		Entry("some message",
			"any message",
			"Reconcile failed",
		),
		Entry("multi-line error message",
			"\n\tstatus code: 301, request id: , host id: ",
			"Reconcile failed",
		),
	)
})
