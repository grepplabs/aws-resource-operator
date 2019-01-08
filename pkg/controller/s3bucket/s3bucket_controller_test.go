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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/mock/gomock"
	"golang.org/x/net/context"
	"strings"
	"sync"
	"time"

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
		invalidRequest  = reconcile.Request{NamespacedName: types.NamespacedName{Name: "s3-foo-invalid", Namespace: "default"}}

		instance *awsv1beta1.S3Bucket

		mockCtrl  *gomock.Controller
		mockS3API *MockS3API
	)
	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockS3API = NewMockS3API(mockCtrl)

		cfg.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		mgr, err := manager.New(cfg, manager.Options{})
		Expect(err).NotTo(HaveOccurred())

		c = mgr.GetClient()

		var recFn reconcile.Reconciler
		recFn, requests = SetupTestReconcile(newTestReconciler(mgr, mockS3API))
		Expect(add(mgr, recFn)).NotTo(HaveOccurred())

		stopMgr, mgrStopped = StartTestManager(mgr)

	})
	Context("Create S3Bucket validation fails", func() {
		AfterEach(func() {
			Expect(c.Delete(context.TODO(), instance)).NotTo(HaveOccurred())
			Eventually(requests, timeout).Should(Receive(Equal(invalidRequest)))
		})
		It("Reject object with bucket name", func() {
			instance = &awsv1beta1.S3Bucket{ObjectMeta: metav1.ObjectMeta{Name: "s3-foo-invalid", Namespace: "default"}}
			err := c.Create(context.TODO(), instance)
			Expect(err).NotTo(HaveOccurred())
			Eventually(requests, timeout).Should(Receive(Equal(invalidRequest)))
		})
	})
	Context("Create S3Bucket", func() {
		var (
			instance *awsv1beta1.S3Bucket
		)
		AfterEach(func() {
			Expect(c.Delete(context.TODO(), instance)).NotTo(HaveOccurred())
			Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
		})
		It("Create S3Bucket with default attributes", func() {
			mockS3API.EXPECT().HeadBucket(
				&s3.HeadBucketInput{
					Bucket: aws.String("test-bucket"),
				}).Return(nil, awserr.NewRequestFailure(awserr.New("NotFound", "Not Found", fmt.Errorf("some error")), 404, "F94AFE7597CFC78F"))

			mockS3API.EXPECT().CreateBucket(
				&s3.CreateBucketInput{
					Bucket: aws.String("test-bucket"),
					ACL:    aws.String("private"),
					CreateBucketConfiguration: &s3.CreateBucketConfiguration{
						LocationConstraint: aws.String("eu-central-1"),
					},
				}).Return(&s3.CreateBucketOutput{Location: aws.String("eu-central-1")}, nil)

			mockS3API.EXPECT().GetBucketPolicy(
				&s3.GetBucketPolicyInput{
					Bucket: aws.String("test-bucket"),
				}).Return(&s3.GetBucketPolicyOutput{Policy: aws.String("")}, nil).MinTimes(1)

			mockS3API.EXPECT().HeadBucket(
				&s3.HeadBucketInput{
					Bucket: aws.String("test-bucket"),
				}).Return(&s3.HeadBucketOutput{}, nil).MinTimes(1)

			instance = &awsv1beta1.S3Bucket{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-foo-bucket",
					Namespace: "default",
				},
				Spec: awsv1beta1.S3BucketSpec{
					Bucket: "test-bucket",
				},
			}

			//TODO: validate eventing

			err := c.Create(context.TODO(), instance)
			Expect(err).NotTo(HaveOccurred())
			Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
		})
	})

	AfterEach(func() {
		close(stopMgr)
		mgrStopped.Wait()
		mockCtrl.Finish()
	})
})

var _ = Describe("S3Bucket Help Functions Suite", func() {
	DescribeTable("S3 event message from error",
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

	DescribeTable("Validate bucket name - valid dns names ",
		func(bucket string) {
			err := validateBucketName(bucket, "us-west-2")
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("foobar", "foobar"),
		Entry("foo.bar", "foo.bar"),
		Entry("foo.bar.baz", "foo.bar.baz"),
		Entry("1234", "1234"),
		Entry("foo-bar", "foo-bar"),
		Entry("x * 63", strings.Repeat("x", 63)),
	)

	DescribeTable("Validate bucket name - invalid dns names ",
		func(bucket string) {
			err := validateBucketName(bucket, "us-west-2")
			Expect(err).To(HaveOccurred())
		},
		Entry("foo..bar", "foo..bar"),
		Entry("Foo.Bar", "Foo.Bar"),
		Entry("192.168.0.1", "192.168.0.1"),
		Entry("127.0.0.1", "127.0.0.1"),
		Entry(".foo", ".foo"),
		Entry("bar.", "bar."),
		Entry("foo_bar", "foo_bar"),
		Entry("x * 64", strings.Repeat("x", 64)),
	)

	DescribeTable("Validate bucket name - valid east names ",
		func(bucket string) {
			err := validateBucketName(bucket, "us-east-1")
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("foobar", "foobar"),
		Entry("foo_bar", "foo_bar"),
		Entry("127.0.0.1", "127.0.0.1"),
		Entry("foo..bar", "foo..bar"),
		Entry("foo_bar_baz", "foo_bar_baz"),
		Entry("foo.bar.baz", "foo.bar.baz"),
		Entry("Foo.Bar", "Foo.Bar"),
		Entry("foobar", "foobar"),
		Entry("x * 255", strings.Repeat("x", 255)),
	)

	DescribeTable("Validate bucket name - invalid east names ",
		func(bucket string) {
			err := validateBucketName(bucket, "us-east-1")
			Expect(err).To(HaveOccurred())
		},
		Entry("foo;bar", "foo;bar"),
		Entry("x * 256", strings.Repeat("x", 256)),
	)
})
