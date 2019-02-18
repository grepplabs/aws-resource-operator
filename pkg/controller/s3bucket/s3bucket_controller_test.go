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
	"k8s.io/api/core/v1"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"k8s.io/client-go/util/flowcontrol"

	awsv1beta1 "github.com/grepplabs/aws-resource-operator/pkg/apis/aws/v1beta1"
	testevents "github.com/grepplabs/aws-resource-operator/test/events"

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
		c          client.Client
		requests   chan reconcile.Request
		stopMgr    chan struct{}
		mgrStopped *sync.WaitGroup

		expectedRequest reconcile.Request
		objectName      string

		testEvents chan TestEvent

		instance *awsv1beta1.S3Bucket

		mockCtrl  *gomock.Controller
		mockS3API *MockS3API

		shouldSendEvent = func(kind, namespace, name, reason string) {
			events := &v1.EventList{}
			filter := func(r, k, n string) func(v1.Event) bool {
				return func(ev v1.Event) bool {
					return ev.Reason == r && ev.InvolvedObject.Kind == k && ev.InvolvedObject.Name == n
				}
			}

			Eventually(func() error {
				err := c.List(context.TODO(), &client.ListOptions{}, events)
				if err != nil {
					return err
				}
				if testevents.None(events.Items, filter(reason, kind, name)) {
					return fmt.Errorf("events haven't been sent yet")
				}
				return nil
			}, timeout).Should(Succeed())

			bucketCreatedEvents := testevents.Select(events.Items, filter("BucketCreated", kind, name))
			Expect(bucketCreatedEvents).ToNot(BeEmpty())
			for _, e := range bucketCreatedEvents {
				Expect(e.InvolvedObject.Kind).To(Equal(kind))
				Expect(e.InvolvedObject.Name).To(Equal(name))
				Expect(e.InvolvedObject.Namespace).To(Equal(namespace))
				Expect(e.Type).To(Equal(string(v1.EventTypeNormal)))
			}
		}
		shouldWaitForStatusUpdate = func(key types.NamespacedName, status awsv1beta1.S3BucketStatus) {
			var obj = &awsv1beta1.S3Bucket{}
			Eventually(func() error {
				err := c.Get(context.TODO(), key, obj)
				if err != nil {
					return err
				}
				if obj.Status != status {
					return fmt.Errorf("status not updated")
				}
				return nil
			}, timeout).Should(Succeed())
		}
		mockHeadBucketNotFound = func() {
			mockS3API.EXPECT().HeadBucket(
				&s3.HeadBucketInput{
					Bucket: aws.String("test-bucket"),
				}).Return(nil, awserr.NewRequestFailure(awserr.New("NotFound", "Not Found", fmt.Errorf("some error")), 404, "F94AFE7597CFC78F"))
		}
		mockCreateBucket = func() {
			mockS3API.EXPECT().CreateBucket(
				&s3.CreateBucketInput{
					Bucket: aws.String("test-bucket"),
					ACL:    aws.String("private"),
					CreateBucketConfiguration: &s3.CreateBucketConfiguration{
						LocationConstraint: aws.String("eu-central-1"),
					},
				}).Return(&s3.CreateBucketOutput{Location: aws.String("eu-central-1")}, nil)
		}
		mockGetBucketTagging = func(tags map[string]string, times int) {
			mockS3API.EXPECT().GetBucketTagging(
				&s3.GetBucketTaggingInput{
					Bucket: aws.String("test-bucket"),
				}).Return(&s3.GetBucketTaggingOutput{TagSet: tagsFromMapS3(tags)}, nil).Times(times)
		}
		mockGetBucketPolicy = func(policy string, times int) {
			mockS3API.EXPECT().GetBucketPolicy(
				&s3.GetBucketPolicyInput{
					Bucket: aws.String("test-bucket"),
				}).Return(&s3.GetBucketPolicyOutput{Policy: aws.String(policy)}, nil).Times(times)
		}
		mockHeadBucket = func(times int) {
			mockS3API.EXPECT().HeadBucket(
				&s3.HeadBucketInput{
					Bucket: aws.String("test-bucket"),
				}).Return(&s3.HeadBucketOutput{}, nil).Times(times)
		}
		mockGetBucketEncryption = func(sseAlgorithm, kmsMasterKeyID string, times int) {
			var serverSideEncryptionConfiguration *s3.ServerSideEncryptionConfiguration
			if sseAlgorithm != "" {
				serverSideEncryptionConfiguration = &s3.ServerSideEncryptionConfiguration{
					Rules: []*s3.ServerSideEncryptionRule{
						{
							ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
								SSEAlgorithm:   aws.String(sseAlgorithm),
								KMSMasterKeyID: aws.String(kmsMasterKeyID),
							},
						},
					},
				}
			}
			mockS3API.EXPECT().GetBucketEncryption(&s3.GetBucketEncryptionInput{
				Bucket: aws.String("test-bucket"),
			}).Return(&s3.GetBucketEncryptionOutput{ServerSideEncryptionConfiguration: serverSideEncryptionConfiguration}, nil).Times(times)
		}

		mockPutBucketEncryption = func(sseAlgorithm string, kmsMasterKeyID *string) {
			mockS3API.EXPECT().PutBucketEncryption(
				&s3.PutBucketEncryptionInput{
					Bucket: aws.String("test-bucket"),
					ServerSideEncryptionConfiguration: &s3.ServerSideEncryptionConfiguration{
						Rules: []*s3.ServerSideEncryptionRule{
							{
								ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
									SSEAlgorithm:   aws.String(sseAlgorithm),
									KMSMasterKeyID: kmsMasterKeyID,
								},
							},
						},
					},
				}).Return(&s3.PutBucketEncryptionOutput{}, nil)
		}

		mockGetBucketVersioning = func(enabled bool, times int) {
			mockS3API.EXPECT().GetBucketVersioning(&s3.GetBucketVersioningInput{
				Bucket: aws.String("test-bucket"),
			}).Return(&s3.GetBucketVersioningOutput{Status: aws.String(bucketVersioningStatus(enabled))}, nil).Times(times)
		}

		mockPutBucketVersioning = func(enabled bool) {
			mockS3API.EXPECT().PutBucketVersioning(&s3.PutBucketVersioningInput{
				Bucket: aws.String("test-bucket"),
				VersioningConfiguration: &s3.VersioningConfiguration{
					Status: aws.String(bucketVersioningStatus(enabled)),
				},
			}).Return(&s3.PutBucketVersioningOutput{}, nil)
		}

		mockGetBucketLogging = func(targetBucket, targetPrefix string, times int) {
			var loggingEnabled *s3.LoggingEnabled
			if targetBucket != "" {
				loggingEnabled = &s3.LoggingEnabled{
					TargetBucket: aws.String(targetBucket),
					TargetPrefix: aws.String(targetPrefix),
				}
			}
			mockS3API.EXPECT().GetBucketLogging(&s3.GetBucketLoggingInput{
				Bucket: aws.String("test-bucket"),
			}).Return(&s3.GetBucketLoggingOutput{LoggingEnabled: loggingEnabled}, nil).Times(times)
		}

		mockPutBucketLogging = func(targetBucket, targetPrefix string) {
			var loggingEnabled *s3.LoggingEnabled
			if targetBucket != "" {
				loggingEnabled = &s3.LoggingEnabled{
					TargetBucket: aws.String(targetBucket),
					TargetPrefix: aws.String(targetPrefix),
				}
			}

			mockS3API.EXPECT().PutBucketLogging(
				&s3.PutBucketLoggingInput{
					Bucket: aws.String("test-bucket"),
					BucketLoggingStatus: &s3.BucketLoggingStatus{
						LoggingEnabled: loggingEnabled,
					},
				}).Return(&s3.PutBucketLoggingOutput{}, nil)
		}
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
		testReconciler := newTestReconciler(mgr, mockS3API)
		recFn, testEvents = SetupTestEventRecorder(testReconciler)
		_ = testEvents
		recFn, requests = SetupTestReconcile(recFn)
		Expect(add(mgr, recFn)).NotTo(HaveOccurred())

		stopMgr, mgrStopped = StartTestManager(mgr)

	})
	JustBeforeEach(func() {
		expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: objectName, Namespace: "default"}}
	})
	Context("Create S3Bucket validation fails", func() {
		BeforeEach(func() {
			objectName = "s3-foo-invalid"
		})
		AfterEach(func() {
			Expect(c.Delete(context.TODO(), instance)).NotTo(HaveOccurred())
			Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
		})
		It("Reject object with bucket name", func() {
			instance = &awsv1beta1.S3Bucket{ObjectMeta: metav1.ObjectMeta{Name: objectName, Namespace: "default"}}
			err := c.Create(context.TODO(), instance)
			Expect(err).NotTo(HaveOccurred())
			Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
		})
	})
	Context("Create S3Bucket", func() {
		var (
			instance *awsv1beta1.S3Bucket
		)
		AfterEach(func() {
			mockS3API.EXPECT().DeleteBucket(&s3.DeleteBucketInput{Bucket: aws.String("test-bucket")}).Return(&s3.DeleteBucketOutput{}, nil)
			Expect(c.Delete(context.TODO(), instance)).NotTo(HaveOccurred())
			Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
		})
		Context("S3Bucket with default attributes", func() {
			BeforeEach(func() {
				objectName = "s3-foo-bucket-1"
			})
			It("Create S3Bucket with default attributes", func() {
				mockHeadBucketNotFound()
				mockCreateBucket()
				mockGetBucketTagging(map[string]string{}, 2)
				mockGetBucketEncryption("", "", 2)
				mockGetBucketVersioning(false, 2)
				mockGetBucketLogging("", "", 2)
				mockGetBucketPolicy("", 2)
				mockHeadBucket(1)

				instance = &awsv1beta1.S3Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectName,
						Namespace: "default",
					},
					Spec: awsv1beta1.S3BucketSpec{
						Bucket: "test-bucket",
					},
				}
				err := c.Create(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

				shouldSendEvent("S3Bucket", "default", objectName, "BucketCreated")

				shouldWaitForStatusUpdate(expectedRequest.NamespacedName,
					awsv1beta1.S3BucketStatus{
						ARN:                "arn:aws:s3:::test-bucket",
						LocationConstraint: "eu-central-1",
						Acl:                "private"})

			})
		})
		Context("S3Bucket with policy", func() {
			BeforeEach(func() {
				objectName = "s3-foo-bucket-2"
			})
			It("Create, update and delete S3Bucket policy", func() {
				mockHeadBucketNotFound()
				mockCreateBucket()
				mockHeadBucket(3)
				mockGetBucketTagging(map[string]string{}, 4)
				mockGetBucketEncryption("", "", 4)
				mockGetBucketVersioning(false, 4)
				mockGetBucketLogging("", "", 4)
				mockGetBucketPolicy("", 1)

				policyAllow := `{"Version": "2012-10-17","Statement": [{"Effect": "Allow"}]}`
				mockS3API.EXPECT().PutBucketPolicy(
					&s3.PutBucketPolicyInput{
						Bucket: aws.String("test-bucket"),
						Policy: aws.String(policyAllow),
					}).Return(&s3.PutBucketPolicyOutput{}, nil)

				mockGetBucketPolicy(policyAllow, 1)

				instance = &awsv1beta1.S3Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectName,
						Namespace: "default",
					},
					Spec: awsv1beta1.S3BucketSpec{
						Bucket: "test-bucket",
						Policy: policyAllow,
					},
				}
				err := c.Create(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

				shouldWaitForStatusUpdate(expectedRequest.NamespacedName,
					awsv1beta1.S3BucketStatus{
						ARN:                "arn:aws:s3:::test-bucket",
						LocationConstraint: "eu-central-1",
						Acl:                "private"})

				shouldSendEvent("S3Bucket", "default", objectName, "BucketCreated")
				shouldSendEvent("S3Bucket", "default", objectName, "BucketPolicyCreated")

				// UPDATE policy
				mockGetBucketPolicy(policyAllow, 1)

				policyDisallow := `{"Version": "2012-10-17","Statement": [{"Effect": "Disallow"}]}`
				mockS3API.EXPECT().PutBucketPolicy(
					&s3.PutBucketPolicyInput{
						Bucket: aws.String("test-bucket"),
						Policy: aws.String(policyDisallow),
					}).Return(&s3.PutBucketPolicyOutput{}, nil)

				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())
				instance.Spec.Policy = policyDisallow
				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for update reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketPolicyUpdated")

				// DELETE policy
				mockGetBucketPolicy(policyDisallow, 1)

				mockS3API.EXPECT().DeleteBucketPolicy(
					&s3.DeleteBucketPolicyInput{
						Bucket: aws.String("test-bucket"),
					}).Return(&s3.DeleteBucketPolicyOutput{}, nil)

				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())
				instance.Spec.Policy = ""
				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketPolicyDeleted")
			})
		})
		Context("S3Bucket ACL", func() {
			BeforeEach(func() {
				objectName = "s3-foo-bucket-3"
			})
			It("Update S3Bucket canned ACL", func() {
				mockHeadBucketNotFound()
				mockCreateBucket()
				mockGetBucketTagging(map[string]string{}, 4)
				mockGetBucketEncryption("", "", 4)
				mockGetBucketVersioning(false, 4)
				mockGetBucketLogging("", "", 4)
				mockGetBucketPolicy("", 4)
				mockHeadBucket(3)

				instance = &awsv1beta1.S3Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectName,
						Namespace: "default",
					},
					Spec: awsv1beta1.S3BucketSpec{
						Bucket: "test-bucket",
					},
				}
				err := c.Create(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

				shouldSendEvent("S3Bucket", "default", objectName, "BucketCreated")

				shouldWaitForStatusUpdate(expectedRequest.NamespacedName,
					awsv1beta1.S3BucketStatus{
						ARN:                "arn:aws:s3:::test-bucket",
						LocationConstraint: "eu-central-1",
						Acl:                "private"})

				// UPDATE ACL
				mockS3API.EXPECT().PutBucketAcl(
					&s3.PutBucketAclInput{
						Bucket: aws.String("test-bucket"),
						ACL:    aws.String("public-read"),
					}).Return(&s3.PutBucketAclOutput{}, nil)

				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())
				instance.Spec.Acl = "public-read"
				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "PutBucketAcl")
			})
		})
		Context("S3Bucket with tagging", func() {
			BeforeEach(func() {
				objectName = "s3-foo-bucket-4"
			})
			It("Create, update and delete S3Bucket tagging", func() {
				mockHeadBucketNotFound()
				mockCreateBucket()
				mockHeadBucket(3)
				mockGetBucketTagging(map[string]string{}, 1)
				mockGetBucketPolicy("", 4)
				mockGetBucketEncryption("", "", 4)
				mockGetBucketVersioning(false, 4)
				mockGetBucketLogging("", "", 4)

				tagsCreate := map[string]string{"tag1": "value1	"}
				mockS3API.EXPECT().PutBucketTagging(
					&s3.PutBucketTaggingInput{
						Bucket: aws.String("test-bucket"),
						Tagging: &s3.Tagging{
							TagSet: tagsFromMapS3(tagsCreate),
						},
					}).Return(&s3.PutBucketTaggingOutput{}, nil)

				mockGetBucketTagging(tagsCreate, 1)
				instance = &awsv1beta1.S3Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectName,
						Namespace: "default",
					},
					Spec: awsv1beta1.S3BucketSpec{
						Bucket: "test-bucket",
						Tags:   tagsCreate,
					},
				}
				err := c.Create(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

				shouldWaitForStatusUpdate(expectedRequest.NamespacedName,
					awsv1beta1.S3BucketStatus{
						ARN:                "arn:aws:s3:::test-bucket",
						LocationConstraint: "eu-central-1",
						Acl:                "private"})

				shouldSendEvent("S3Bucket", "default", objectName, "BucketCreated")
				shouldSendEvent("S3Bucket", "default", objectName, "BucketTaggingCreated")

				// UPDATE tagging
				mockGetBucketTagging(tagsCreate, 1)

				tagsUpdate := map[string]string{"tag2": "value2"}

				mockS3API.EXPECT().DeleteBucketTagging(
					&s3.DeleteBucketTaggingInput{
						Bucket: aws.String("test-bucket"),
					}).Return(&s3.DeleteBucketTaggingOutput{}, nil)

				mockS3API.EXPECT().PutBucketTagging(
					&s3.PutBucketTaggingInput{
						Bucket: aws.String("test-bucket"),
						Tagging: &s3.Tagging{
							TagSet: tagsFromMapS3(tagsUpdate),
						},
					}).Return(&s3.PutBucketTaggingOutput{}, nil)

				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())
				instance.Spec.Tags = tagsUpdate
				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for update reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketTaggingUpdated")

				// DELETE tagging
				mockGetBucketTagging(tagsUpdate, 1)

				mockS3API.EXPECT().DeleteBucketTagging(
					&s3.DeleteBucketTaggingInput{
						Bucket: aws.String("test-bucket"),
					}).Return(&s3.DeleteBucketTaggingOutput{}, nil)

				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())
				instance.Spec.Tags = map[string]string{}
				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketTaggingDeleted")
			})
		})
		Context("S3Bucket with SSE config", func() {
			BeforeEach(func() {
				objectName = "s3-foo-bucket-5"
			})
			It("Create, update and delete S3Bucket tagging", func() {
				mockHeadBucketNotFound()
				mockCreateBucket()
				mockHeadBucket(3)
				mockGetBucketTagging(map[string]string{}, 4)
				mockGetBucketPolicy("", 4)
				mockGetBucketEncryption("", "", 1)
				mockGetBucketVersioning(false, 4)
				mockGetBucketLogging("", "", 4)

				mockGetBucketEncryption("AES256", "", 1)
				mockPutBucketEncryption("AES256", nil)
				instance = &awsv1beta1.S3Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectName,
						Namespace: "default",
					},
					Spec: awsv1beta1.S3BucketSpec{
						Bucket: "test-bucket",
						ServerSideEncryptionConfiguration: &awsv1beta1.S3BucketServerSideEncryptionConfiguration{
							Rule: awsv1beta1.S3ServerSideEncryptionRule{
								ApplyServerSideEncryptionByDefault: awsv1beta1.S3ServerSideEncryptionByDefault{
									SSEAlgorithm: "AES256",
								},
							},
						},
					},
				}
				err := c.Create(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

				shouldWaitForStatusUpdate(expectedRequest.NamespacedName,
					awsv1beta1.S3BucketStatus{
						ARN:                "arn:aws:s3:::test-bucket",
						LocationConstraint: "eu-central-1",
						Acl:                "private"})

				shouldSendEvent("S3Bucket", "default", objectName, "BucketCreated")
				shouldSendEvent("S3Bucket", "default", objectName, "BucketEncryptionCreated")

				// UPDATE SSE config
				mockGetBucketEncryption("AES256", "", 1)
				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())

				mockPutBucketEncryption("aws:kms", aws.String("arn:aws:kms:eu-central-1:1234/5678example"))
				instance.Spec.ServerSideEncryptionConfiguration = &awsv1beta1.S3BucketServerSideEncryptionConfiguration{
					Rule: awsv1beta1.S3ServerSideEncryptionRule{
						ApplyServerSideEncryptionByDefault: awsv1beta1.S3ServerSideEncryptionByDefault{
							SSEAlgorithm:   "aws:kms",
							KMSMasterKeyID: "arn:aws:kms:eu-central-1:1234/5678example",
						},
					},
				}

				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for update reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketEncryptionUpdated")

				// DELETE tagging
				mockGetBucketEncryption("aws:kms", "arn:aws:kms:eu-central-1:1234/5678example", 1)

				mockS3API.EXPECT().DeleteBucketEncryption(
					&s3.DeleteBucketEncryptionInput{
						Bucket: aws.String("test-bucket"),
					}).Return(&s3.DeleteBucketEncryptionOutput{}, nil)

				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())
				instance.Spec.ServerSideEncryptionConfiguration = nil
				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketEncryptionDeleted")
			})
		})
		Context("S3Bucket versioning", func() {
			BeforeEach(func() {
				objectName = "s3-foo-bucket-6"
			})
			It("Update S3Bucket versioning", func() {
				mockHeadBucketNotFound()
				mockCreateBucket()
				mockHeadBucket(2)
				mockGetBucketTagging(map[string]string{}, 3)
				mockGetBucketPolicy("", 3)
				mockGetBucketEncryption("", "", 3)
				mockGetBucketVersioning(false, 1)
				mockGetBucketLogging("", "", 3)

				mockPutBucketVersioning(true)
				mockGetBucketVersioning(true, 1)

				instance = &awsv1beta1.S3Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectName,
						Namespace: "default",
					},
					Spec: awsv1beta1.S3BucketSpec{
						Bucket: "test-bucket",
						Versioning: &awsv1beta1.S3BucketVersioning{
							Enabled: true,
						},
					},
				}
				err := c.Create(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

				shouldWaitForStatusUpdate(expectedRequest.NamespacedName,
					awsv1beta1.S3BucketStatus{
						ARN:                "arn:aws:s3:::test-bucket",
						LocationConstraint: "eu-central-1",
						Acl:                "private"})

				shouldSendEvent("S3Bucket", "default", objectName, "BucketCreated")
				shouldSendEvent("S3Bucket", "default", objectName, "BucketVersioningEnabled")

				// UPDATE versioning
				mockGetBucketVersioning(true, 1)
				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())

				mockPutBucketVersioning(false)
				instance.Spec.Versioning.Enabled = false

				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for update reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketVersioningSuspended")
			})
		})
		Context("S3Bucket with logging config", func() {
			BeforeEach(func() {
				objectName = "s3-foo-bucket-7"
			})
			It("Create, update and delete S3Bucket with logging", func() {
				mockHeadBucketNotFound()
				mockCreateBucket()
				mockHeadBucket(3)
				mockGetBucketTagging(map[string]string{}, 4)
				mockGetBucketPolicy("", 4)
				mockGetBucketEncryption("", "", 4)
				mockGetBucketVersioning(false, 4)
				mockGetBucketLogging("", "", 1)

				mockPutBucketLogging("logging-bucket1", "")
				mockGetBucketLogging("logging-bucket1", "", 1)
				instance = &awsv1beta1.S3Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectName,
						Namespace: "default",
					},
					Spec: awsv1beta1.S3BucketSpec{
						Bucket: "test-bucket",
						Logging: &awsv1beta1.S3BucketLogging{
							TargetBucket: "logging-bucket1",
						},
					},
				}
				err := c.Create(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for create reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				// wait for reconcile of status
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))

				shouldWaitForStatusUpdate(expectedRequest.NamespacedName,
					awsv1beta1.S3BucketStatus{
						ARN:                "arn:aws:s3:::test-bucket",
						LocationConstraint: "eu-central-1",
						Acl:                "private"})

				shouldSendEvent("S3Bucket", "default", objectName, "BucketCreated")
				shouldSendEvent("S3Bucket", "default", objectName, "BucketLoggingCreated")

				// UPDATE logging config
				mockGetBucketLogging("logging-bucket1", "", 1)
				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())

				mockPutBucketLogging("logging-bucket2", "prefix2")
				instance.Spec.Logging = &awsv1beta1.S3BucketLogging{
					TargetBucket: "logging-bucket2",
					TargetPrefix: "prefix2",
				}

				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				// wait for update reconcile
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketLoggingUpdated")

				// DELETE tagging
				mockGetBucketLogging("logging-bucket2", "prefix2", 1)
				mockPutBucketLogging("", "")

				// get latest version of the object
				Eventually(func() error { return c.Get(context.TODO(), expectedRequest.NamespacedName, instance) }, timeout).Should(Succeed())
				instance.Spec.Logging = nil
				err = c.Update(context.TODO(), instance)
				Expect(err).NotTo(HaveOccurred())
				Eventually(requests, timeout).Should(Receive(Equal(expectedRequest)))
				shouldSendEvent("S3Bucket", "default", objectName, "BucketLoggingDeleted")
			})
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

	DescribeTable("S3 bucket versioning status",
		func(enabled bool, status string) {
			Expect(bucketVersioningStatus(enabled)).To(Equal(status))
		},
		Entry("enabled",
			true,
			s3.BucketVersioningStatusEnabled,
		),
		Entry("suspended",
			false,
			s3.BucketVersioningStatusSuspended,
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
