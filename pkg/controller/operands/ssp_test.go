package operands

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"

	hcov1beta1 "github.com/kubevirt/hyperconverged-cluster-operator/pkg/apis/hco/v1beta1"
	"github.com/kubevirt/hyperconverged-cluster-operator/pkg/controller/common"
	"github.com/kubevirt/hyperconverged-cluster-operator/pkg/controller/commonTestUtils"
	hcoutil "github.com/kubevirt/hyperconverged-cluster-operator/pkg/util"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	lifecycleapi "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/api"
	sspv1beta1 "kubevirt.io/ssp-operator/api/v1beta1"
)

var _ = Describe("SSP Operands", func() {

	var (
		testFilesLocation = getTestFilesLocation() + "/dataImportCronTemplates"
	)
	Context("SSP", func() {
		var hco *hcov1beta1.HyperConverged
		var req *common.HcoRequest

		BeforeEach(func() {
			hco = commonTestUtils.NewHco()
			req = commonTestUtils.NewReq(hco)
		})

		It("should create if not present", func() {
			expectedResource, err := NewSSP(hco)
			Expect(err).ToNot(HaveOccurred())
			cl := commonTestUtils.InitClient([]runtime.Object{})
			handler := newSspHandler(cl, commonTestUtils.GetScheme())
			res := handler.ensure(req)
			Expect(res.Created).To(BeTrue())
			Expect(res.Updated).To(BeFalse())
			Expect(res.Overwritten).To(BeFalse())
			Expect(res.UpgradeDone).To(BeFalse())
			Expect(res.Err).To(BeNil())

			foundResource := &sspv1beta1.SSP{}
			Expect(
				cl.Get(context.TODO(),
					types.NamespacedName{Name: expectedResource.Name, Namespace: expectedResource.Namespace},
					foundResource),
			).To(BeNil())
			Expect(foundResource.Name).To(Equal(expectedResource.Name))
			Expect(foundResource.Labels).Should(HaveKeyWithValue(hcoutil.AppLabel, commonTestUtils.Name))
			Expect(foundResource.Namespace).To(Equal(expectedResource.Namespace))
		})

		It("should find if present", func() {
			expectedResource, err := NewSSP(hco)
			Expect(err).ToNot(HaveOccurred())
			expectedResource.ObjectMeta.SelfLink = fmt.Sprintf("/apis/v1/namespaces/%s/dummies/%s", expectedResource.Namespace, expectedResource.Name)
			cl := commonTestUtils.InitClient([]runtime.Object{hco, expectedResource})
			handler := newSspHandler(cl, commonTestUtils.GetScheme())
			res := handler.ensure(req)
			Expect(res.Created).To(BeFalse())
			Expect(res.Updated).To(BeFalse())
			Expect(res.Overwritten).To(BeFalse())
			Expect(res.UpgradeDone).To(BeFalse())
			Expect(res.Err).To(BeNil())

			// Check HCO's status
			Expect(hco.Status.RelatedObjects).To(Not(BeNil()))
			objectRef, err := reference.GetReference(handler.Scheme, expectedResource)
			Expect(err).To(BeNil())
			// ObjectReference should have been added
			Expect(hco.Status.RelatedObjects).To(ContainElement(*objectRef))
		})

		It("should reconcile to default", func() {
			cTNamespace := "nonDefault"
			hco.Spec.CommonTemplatesNamespace = &cTNamespace
			expectedResource, err := NewSSP(hco)
			Expect(err).ToNot(HaveOccurred())
			existingResource := expectedResource.DeepCopy()
			existingResource.ObjectMeta.SelfLink = fmt.Sprintf("/apis/v1/namespaces/%s/dummies/%s", existingResource.Namespace, existingResource.Name)

			replicas := int32(defaultTemplateValidatorReplicas * 2) // non-default value
			existingResource.Spec.TemplateValidator.Replicas = &replicas
			existingResource.Spec.NodeLabeller.Placement = &lifecycleapi.NodePlacement{
				NodeSelector: map[string]string{"foo": "bar"},
			}

			req.HCOTriggered = false // mock a reconciliation triggered by a change in NewKubeVirtCommonTemplateBundle CR

			cl := commonTestUtils.InitClient([]runtime.Object{hco, existingResource})
			handler := newSspHandler(cl, commonTestUtils.GetScheme())
			res := handler.ensure(req)
			Expect(res.Created).To(BeFalse())
			Expect(res.Updated).To(BeTrue())
			Expect(res.Overwritten).To(BeTrue())
			Expect(res.UpgradeDone).To(BeFalse())
			Expect(res.Err).To(BeNil())

			foundResource := &sspv1beta1.SSP{}
			Expect(
				cl.Get(context.TODO(),
					types.NamespacedName{Name: existingResource.Name, Namespace: existingResource.Namespace},
					foundResource),
			).To(BeNil())
			Expect(foundResource.Spec).To(Equal(expectedResource.Spec))
			Expect(foundResource.Spec.CommonTemplates.Namespace).To(Equal(cTNamespace), "common-templates namespace should equal")

			// ObjectReference should have been updated
			Expect(hco.Status.RelatedObjects).To(Not(BeNil()))
			objectRefOutdated, err := reference.GetReference(handler.Scheme, existingResource)
			Expect(err).To(BeNil())
			objectRefFound, err := reference.GetReference(handler.Scheme, foundResource)
			Expect(err).To(BeNil())
			Expect(hco.Status.RelatedObjects).To(Not(ContainElement(*objectRefOutdated)))
			Expect(hco.Status.RelatedObjects).To(ContainElement(*objectRefFound))
		})

		Context("Node placement", func() {

			It("should add node placement if missing", func() {
				existingResource, err := NewSSP(hco, commonTestUtils.Namespace)
				Expect(err).ToNot(HaveOccurred())

				hco.Spec.Workloads.NodePlacement = commonTestUtils.NewNodePlacement()
				hco.Spec.Infra.NodePlacement = commonTestUtils.NewOtherNodePlacement()

				cl := commonTestUtils.InitClient([]runtime.Object{hco, existingResource})
				handler := newSspHandler(cl, commonTestUtils.GetScheme())
				res := handler.ensure(req)
				Expect(res.Created).To(BeFalse())
				Expect(res.Updated).To(BeTrue())
				Expect(res.Overwritten).To(BeFalse())
				Expect(res.UpgradeDone).To(BeFalse())
				Expect(res.Err).To(BeNil())

				foundResource := &sspv1beta1.SSP{}
				Expect(
					cl.Get(context.TODO(),
						types.NamespacedName{Name: existingResource.Name, Namespace: existingResource.Namespace},
						foundResource),
				).To(BeNil())

				Expect(existingResource.Spec.NodeLabeller.Placement).To(BeZero())
				Expect(existingResource.Spec.TemplateValidator.Placement).To(BeZero())
				Expect(*foundResource.Spec.NodeLabeller.Placement).To(Equal(*hco.Spec.Workloads.NodePlacement))
				Expect(*foundResource.Spec.TemplateValidator.Placement).To(Equal(*hco.Spec.Infra.NodePlacement))
				Expect(req.Conditions).To(BeEmpty())
			})

			It("should remove node placement if missing in HCO CR", func() {

				hcoNodePlacement := commonTestUtils.NewHco()
				hcoNodePlacement.Spec.Workloads.NodePlacement = commonTestUtils.NewNodePlacement()
				hcoNodePlacement.Spec.Infra.NodePlacement = commonTestUtils.NewOtherNodePlacement()
				existingResource, err := NewSSP(hcoNodePlacement, commonTestUtils.Namespace)
				Expect(err).ToNot(HaveOccurred())

				cl := commonTestUtils.InitClient([]runtime.Object{hco, existingResource})
				handler := newSspHandler(cl, commonTestUtils.GetScheme())
				res := handler.ensure(req)
				Expect(res.Created).To(BeFalse())
				Expect(res.Updated).To(BeTrue())
				Expect(res.Overwritten).To(BeFalse())
				Expect(res.UpgradeDone).To(BeFalse())
				Expect(res.Err).To(BeNil())

				foundResource := &sspv1beta1.SSP{}
				Expect(
					cl.Get(context.TODO(),
						types.NamespacedName{Name: existingResource.Name, Namespace: existingResource.Namespace},
						foundResource),
				).To(BeNil())

				Expect(existingResource.Spec.NodeLabeller.Placement).ToNot(BeZero())
				Expect(existingResource.Spec.TemplateValidator.Placement).ToNot(BeZero())
				Expect(foundResource.Spec.NodeLabeller.Placement).To(BeZero())
				Expect(foundResource.Spec.TemplateValidator.Placement).To(BeZero())
				Expect(req.Conditions).To(BeEmpty())
			})

			It("should modify node placement according to HCO CR", func() {

				hco.Spec.Workloads.NodePlacement = commonTestUtils.NewNodePlacement()
				hco.Spec.Infra.NodePlacement = commonTestUtils.NewOtherNodePlacement()
				existingResource, err := NewSSP(hco, commonTestUtils.Namespace)
				Expect(err).ToNot(HaveOccurred())

				// now, modify HCO's node placement
				seconds12 := int64(12)
				hco.Spec.Workloads.NodePlacement.Tolerations = append(hco.Spec.Workloads.NodePlacement.Tolerations, corev1.Toleration{
					Key: "key12", Operator: "operator12", Value: "value12", Effect: "effect12", TolerationSeconds: &seconds12,
				})
				hco.Spec.Workloads.NodePlacement.NodeSelector["key1"] = "something else"

				seconds34 := int64(34)
				hco.Spec.Infra.NodePlacement.Tolerations = append(hco.Spec.Infra.NodePlacement.Tolerations, corev1.Toleration{
					Key: "key34", Operator: "operator34", Value: "value34", Effect: "effect34", TolerationSeconds: &seconds34,
				})
				hco.Spec.Infra.NodePlacement.NodeSelector["key3"] = "something entirely else"

				cl := commonTestUtils.InitClient([]runtime.Object{hco, existingResource})
				handler := newSspHandler(cl, commonTestUtils.GetScheme())
				res := handler.ensure(req)
				Expect(res.Created).To(BeFalse())
				Expect(res.Updated).To(BeTrue())
				Expect(res.Overwritten).To(BeFalse())
				Expect(res.UpgradeDone).To(BeFalse())
				Expect(res.Err).To(BeNil())

				foundResource := &sspv1beta1.SSP{}
				Expect(
					cl.Get(context.TODO(),
						types.NamespacedName{Name: existingResource.Name, Namespace: existingResource.Namespace},
						foundResource),
				).To(BeNil())

				Expect(existingResource.Spec.NodeLabeller.Placement.Affinity.NodeAffinity).ToNot(BeZero())
				Expect(existingResource.Spec.NodeLabeller.Placement.Tolerations).To(HaveLen(2))
				Expect(existingResource.Spec.NodeLabeller.Placement.NodeSelector["key1"]).Should(Equal("value1"))
				Expect(existingResource.Spec.TemplateValidator.Placement.Affinity.NodeAffinity).ToNot(BeZero())
				Expect(existingResource.Spec.TemplateValidator.Placement.Tolerations).To(HaveLen(2))
				Expect(existingResource.Spec.TemplateValidator.Placement.NodeSelector["key3"]).Should(Equal("value3"))

				Expect(foundResource.Spec.NodeLabeller.Placement.Affinity.NodeAffinity).ToNot(BeNil())
				Expect(foundResource.Spec.NodeLabeller.Placement.Tolerations).To(HaveLen(3))
				Expect(foundResource.Spec.NodeLabeller.Placement.NodeSelector["key1"]).Should(Equal("something else"))
				Expect(foundResource.Spec.TemplateValidator.Placement.Affinity.NodeAffinity).ToNot(BeNil())
				Expect(foundResource.Spec.TemplateValidator.Placement.Tolerations).To(HaveLen(3))
				Expect(foundResource.Spec.TemplateValidator.Placement.NodeSelector["key3"]).Should(Equal("something entirely else"))

				Expect(req.Conditions).To(BeEmpty())
			})

			It("should overwrite node placement if directly set on SSP CR", func() {
				hco.Spec.Workloads = hcov1beta1.HyperConvergedConfig{NodePlacement: commonTestUtils.NewNodePlacement()}
				hco.Spec.Infra = hcov1beta1.HyperConvergedConfig{NodePlacement: commonTestUtils.NewOtherNodePlacement()}
				existingResource, err := NewSSP(hco, commonTestUtils.Namespace)
				Expect(err).ToNot(HaveOccurred())

				// mock a reconciliation triggered by a change in NewKubeVirtNodeLabellerBundle CR
				req.HCOTriggered = false

				// now, modify NodeLabeller node placement
				seconds12 := int64(12)
				existingResource.Spec.NodeLabeller.Placement.Tolerations = append(hco.Spec.Workloads.NodePlacement.Tolerations, corev1.Toleration{
					Key: "key12", Operator: "operator12", Value: "value12", Effect: "effect12", TolerationSeconds: &seconds12,
				})
				existingResource.Spec.NodeLabeller.Placement.NodeSelector["key1"] = "BADvalue1"

				// and modify TemplateValidator node placement
				seconds34 := int64(34)
				existingResource.Spec.TemplateValidator.Placement.Tolerations = append(hco.Spec.Infra.NodePlacement.Tolerations, corev1.Toleration{
					Key: "key34", Operator: "operator34", Value: "value34", Effect: "effect34", TolerationSeconds: &seconds34,
				})
				existingResource.Spec.TemplateValidator.Placement.NodeSelector["key3"] = "BADvalue3"

				cl := commonTestUtils.InitClient([]runtime.Object{hco, existingResource})
				handler := newSspHandler(cl, commonTestUtils.GetScheme())
				res := handler.ensure(req)
				Expect(res.UpgradeDone).To(BeFalse())
				Expect(res.Updated).To(BeTrue())
				Expect(res.Overwritten).To(BeTrue())
				Expect(res.Err).To(BeNil())

				foundResource := &sspv1beta1.SSP{}
				Expect(
					cl.Get(context.TODO(),
						types.NamespacedName{Name: existingResource.Name, Namespace: existingResource.Namespace},
						foundResource),
				).To(BeNil())

				Expect(existingResource.Spec.NodeLabeller.Placement.Tolerations).To(HaveLen(3))
				Expect(existingResource.Spec.NodeLabeller.Placement.NodeSelector["key1"]).Should(Equal("BADvalue1"))
				Expect(existingResource.Spec.TemplateValidator.Placement.Tolerations).To(HaveLen(3))
				Expect(existingResource.Spec.TemplateValidator.Placement.NodeSelector["key3"]).Should(Equal("BADvalue3"))

				Expect(foundResource.Spec.NodeLabeller.Placement.Tolerations).To(HaveLen(2))
				Expect(foundResource.Spec.NodeLabeller.Placement.NodeSelector["key1"]).Should(Equal("value1"))
				Expect(foundResource.Spec.TemplateValidator.Placement.Tolerations).To(HaveLen(2))
				Expect(foundResource.Spec.TemplateValidator.Placement.NodeSelector["key3"]).Should(Equal("value3"))

				Expect(req.Conditions).To(BeEmpty())
			})
		})

		Context("Cache", func() {
			cl := commonTestUtils.InitClient([]runtime.Object{})
			handler := newSspHandler(cl, commonTestUtils.GetScheme())

			It("should start with empty cache", func() {
				Expect(handler.hooks.(*sspHooks).cache).To(BeNil())
			})

			It("should update the cache when reading full CR", func() {
				cr, err := handler.hooks.getFullCr(hco)
				Expect(err).ToNot(HaveOccurred())
				Expect(cr).ToNot(BeNil())
				Expect(handler.hooks.(*sspHooks).cache).ToNot(BeNil())

				By("compare pointers to make sure cache is working", func() {
					Expect(handler.hooks.(*sspHooks).cache == cr).Should(BeTrue())

					cdi1, err := handler.hooks.getFullCr(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(cdi1).ToNot(BeNil())
					Expect(cr == cdi1).Should(BeTrue())
				})
			})

			It("should remove the cache on reset", func() {
				handler.hooks.(*sspHooks).reset()
				Expect(handler.hooks.(*sspHooks).cache).To(BeNil())
			})

			It("check that reset actually cause creating of a new cached instance", func() {
				crI, err := handler.hooks.getFullCr(hco)
				Expect(err).ToNot(HaveOccurred())
				Expect(crI).ToNot(BeNil())
				Expect(handler.hooks.(*sspHooks).cache).ToNot(BeNil())

				handler.hooks.(*sspHooks).reset()
				Expect(handler.hooks.(*sspHooks).cache).To(BeNil())

				crII, err := handler.hooks.getFullCr(hco)
				Expect(err).ToNot(HaveOccurred())
				Expect(crII).ToNot(BeNil())
				Expect(handler.hooks.(*sspHooks).cache).ToNot(BeNil())

				Expect(crI == crII).To(BeFalse())
				Expect(handler.hooks.(*sspHooks).cache == crI).To(BeFalse())
				Expect(handler.hooks.(*sspHooks).cache == crII).To(BeTrue())
			})
		})

		Context("Test data import cron template", func() {
			dir := path.Join(os.TempDir(), fmt.Sprint(time.Now().UTC().Unix()))
			origFunc := getDataImportCronTemplatesFileLocation

			url1 := "docker://someregistry/image1"
			url2 := "docker://someregistry/image2"
			url3 := "docker://someregistry/image3"
			url4 := "docker://someregistry/image4"

			image1 := sspv1beta1.DataImportCronTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "image1"},
				Spec: cdiv1beta1.DataImportCronSpec{
					Schedule: "1 */12 * * *",
					Template: cdiv1beta1.DataVolume{
						Spec: cdiv1beta1.DataVolumeSpec{
							Source: &cdiv1beta1.DataVolumeSource{
								Registry: &cdiv1beta1.DataVolumeSourceRegistry{URL: &url1},
							},
						},
					},
					ManagedDataSource: "image1",
				},
			}

			image2 := sspv1beta1.DataImportCronTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "image2"},
				Spec: cdiv1beta1.DataImportCronSpec{
					Schedule: "2 */12 * * *",
					Template: cdiv1beta1.DataVolume{
						Spec: cdiv1beta1.DataVolumeSpec{
							Source: &cdiv1beta1.DataVolumeSource{
								Registry: &cdiv1beta1.DataVolumeSourceRegistry{URL: &url2},
							},
						},
					},
					ManagedDataSource: "image2",
				},
			}

			image3 := sspv1beta1.DataImportCronTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "image3"},
				Spec: cdiv1beta1.DataImportCronSpec{
					Schedule: "3 */12 * * *",
					Template: cdiv1beta1.DataVolume{
						Spec: cdiv1beta1.DataVolumeSpec{
							Source: &cdiv1beta1.DataVolumeSource{
								Registry: &cdiv1beta1.DataVolumeSourceRegistry{URL: &url3},
							},
						},
					},
					ManagedDataSource: "image3",
				},
			}

			image4 := sspv1beta1.DataImportCronTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "image4"},
				Spec: cdiv1beta1.DataImportCronSpec{
					Schedule: "4 */12 * * *",
					Template: cdiv1beta1.DataVolume{
						Spec: cdiv1beta1.DataVolumeSpec{
							Source: &cdiv1beta1.DataVolumeSource{
								Registry: &cdiv1beta1.DataVolumeSourceRegistry{URL: &url4},
							},
						},
					},
					ManagedDataSource: "image4",
				},
			}

			BeforeEach(func() {
				getDataImportCronTemplatesFileLocation = func() string {
					return dir
				}
			})

			AfterEach(func() {
				getDataImportCronTemplatesFileLocation = origFunc
			})

			It("should read the dataImportCronTemplates file", func() {

				By("directory does not exist - no error")
				Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())
				Expect(dataImportCronTemplateHardCodedList).To(BeEmpty())

				By("file does not exist - no error")
				err := os.Mkdir(dir, os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()

				Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())
				Expect(dataImportCronTemplateHardCodedList).To(BeEmpty())

				destFile := path.Join(dir, "dataImportCronTemplates.yaml")

				By("valid file exits")
				err = commonTestUtils.CopyFile(destFile, path.Join(testFilesLocation, "dataImportCronTemplates.yaml"))
				Expect(err).ToNot(HaveOccurred())
				defer os.Remove(destFile)
				Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())
				Expect(dataImportCronTemplateHardCodedList).ToNot(BeNil())
				Expect(dataImportCronTemplateHardCodedList).To(HaveLen(2))

				By("the file is wrong")
				err = commonTestUtils.CopyFile(destFile, path.Join(testFilesLocation, "wrongDataImportCronTemplates.yaml"))
				Expect(err).ToNot(HaveOccurred())
				defer os.Remove(destFile)
				Expect(readDataImportCronTemplatesFromFile()).To(HaveOccurred())
				Expect(dataImportCronTemplateHardCodedList).To(BeEmpty())
			})

			Context("test getDataImportCronTemplates", func() {
				origList := dataImportCronTemplateHardCodedList
				defer func() { dataImportCronTemplateHardCodedList = origList }()

				It("should not return the hard coded list dataImportCron FeatureGate is false", func() {
					hco := commonTestUtils.NewHco()
					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{image1, image2}
					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{image3, image4}
					list, err := getDataImportCronTemplates(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(list).To(HaveLen(2))
					Expect(list).To(ContainElements(image3, image4))

					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{}
					list, err = getDataImportCronTemplates(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(list).To(BeNil())
				})

				It("should return an empty list if both the hard-coded list and the list from HC are empty", func() {
					hcoWithEmptyList := commonTestUtils.NewHco()
					hcoWithEmptyList.Spec.FeatureGates.EnableCommonBootImageImport = true
					hcoWithEmptyList.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{}
					hcoWithNilList := commonTestUtils.NewHco()
					hcoWithNilList.Spec.FeatureGates.EnableCommonBootImageImport = true
					hcoWithNilList.Spec.DataImportCronTemplates = nil

					dataImportCronTemplateHardCodedList = nil
					Expect(getDataImportCronTemplates(hcoWithNilList)).To(BeNil())
					Expect(getDataImportCronTemplates(hcoWithEmptyList)).To(BeNil())
					dataImportCronTemplateHardCodedList = make([]sspv1beta1.DataImportCronTemplate, 0)
					Expect(getDataImportCronTemplates(hcoWithNilList)).To(BeNil())
					Expect(getDataImportCronTemplates(hcoWithEmptyList)).To(BeNil())
				})

				It("Should add the CR list to the hard-coded list", func() {
					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{image1, image2}
					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true
					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{image3, image4}
					goldenImageList, err := getDataImportCronTemplates(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(goldenImageList).To(HaveLen(4))
					Expect(goldenImageList).To(HaveCap(4))
					Expect(goldenImageList).To(ContainElements(image1, image2, image3, image4))
				})

				It("Should reject if the CR list contain DIC template with the same name as in the hard-coded list", func() {
					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{image1, image2}
					dataImportCronTemplateHardCodedNames = map[string]struct{}{
						image1.Name: {},
						image2.Name: {},
					}
					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true

					image3Modified := image3.DeepCopy()
					image3Modified.Name = image2.Name

					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{*image3Modified, image4}
					_, err := getDataImportCronTemplates(hco)
					Expect(err).To(HaveOccurred())
				})

				It("Should reject if the CR list contain DIC templates with the same name, when there are also common DIC templates", func() {
					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{image1, image2}
					dataImportCronTemplateHardCodedNames = map[string]struct{}{
						image1.Name: {},
						image2.Name: {},
					}
					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true

					image3Modified := image3.DeepCopy()
					image3Modified.Name = image4.Name

					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{*image3Modified, image4}
					_, err := getDataImportCronTemplates(hco)
					Expect(err).To(HaveOccurred())
				})

				It("Should reject if the CR list contain DIC templates with the same name", func() {
					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true

					image3Modified := image3.DeepCopy()
					image3Modified.Name = image4.Name

					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{*image3Modified, image4}
					_, err := getDataImportCronTemplates(hco)
					Expect(err).To(HaveOccurred())
				})

				It("Should not add the CR list to the hard-coded list, if it's empty", func() {
					By("CR list is nil")
					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{image1, image2}
					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true
					hco.Spec.DataImportCronTemplates = nil
					goldenImageList, err := getDataImportCronTemplates(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(goldenImageList).To(HaveLen(2))
					Expect(goldenImageList).To(HaveCap(2))
					Expect(goldenImageList).To(ContainElements(image1, image2))

					By("CR list is empty")
					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{}
					goldenImageList, err = getDataImportCronTemplates(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(goldenImageList).To(HaveLen(2))
					Expect(goldenImageList).To(ContainElements(image1, image2))
				})

				It("Should return only the CR list, if the hard-coded list is empty", func() {
					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true
					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{image3, image4}

					By("when dataImportCronTemplateHardCodedList is nil")
					dataImportCronTemplateHardCodedList = nil
					goldenImageList, err := getDataImportCronTemplates(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(goldenImageList).To(HaveLen(2))
					Expect(goldenImageList).To(HaveCap(2))
					Expect(goldenImageList).To(ContainElements(image3, image4))

					By("when dataImportCronTemplateHardCodedList is empty")
					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{}
					goldenImageList, err = getDataImportCronTemplates(hco)
					Expect(err).ToNot(HaveOccurred())
					Expect(goldenImageList).To(HaveLen(2))
					Expect(goldenImageList).To(HaveCap(2))
					Expect(goldenImageList).To(ContainElements(image3, image4))
				})
			})

			Context("test data import cron templates in NewSsp", func() {

				It("should return an empty list if there is no file and no list in the HyperConverged CR", func() {
					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true
					ssp, err := NewSSP(hco)
					Expect(err).ToNot(HaveOccurred())

					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(BeNil())
				})

				It("should return an the hard coded list if there is a file, but no list in the HyperConverged CR", func() {
					err := os.Mkdir(dir, os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					defer func() { _ = os.RemoveAll(dir) }()
					destFile := path.Join(dir, "dataImportCronTemplates.yaml")

					err = commonTestUtils.CopyFile(destFile, path.Join(testFilesLocation, "dataImportCronTemplates.yaml"))
					Expect(err).ToNot(HaveOccurred())
					defer os.Remove(destFile)
					Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())

					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true
					ssp, err := NewSSP(hco)
					Expect(err).ToNot(HaveOccurred())

					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).ShouldNot(BeNil())
					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(HaveLen(2))
				})

				It("should return a combined list if there is a file and a list in the HyperConverged CR", func() {
					err := os.Mkdir(dir, os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					defer func() { _ = os.RemoveAll(dir) }()
					destFile := path.Join(dir, "dataImportCronTemplates.yaml")

					err = commonTestUtils.CopyFile(destFile, path.Join(testFilesLocation, "dataImportCronTemplates.yaml"))
					Expect(err).ToNot(HaveOccurred())
					defer os.Remove(destFile)
					Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())

					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true
					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{image3, image4}
					ssp, err := NewSSP(hco)
					Expect(err).ToNot(HaveOccurred())

					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).ShouldNot(BeNil())
					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(HaveLen(4))
					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(ContainElements(image3, image4))
				})

				It("Should reject if the CR list contain DIC template with the same name as in the hard-coded list", func() {
					err := os.Mkdir(dir, os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					defer func() { _ = os.RemoveAll(dir) }()
					destFile := path.Join(dir, "dataImportCronTemplates.yaml")

					err = commonTestUtils.CopyFile(destFile, path.Join(testFilesLocation, "dataImportCronTemplates.yaml"))
					Expect(err).ToNot(HaveOccurred())
					defer os.Remove(destFile)
					Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())

					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true

					Expect(dataImportCronTemplateHardCodedList).ToNot(BeEmpty())
					image3Modified := image3.DeepCopy()
					image3Modified.Name = dataImportCronTemplateHardCodedList[0].Name

					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{*image3Modified, image4}
					ssp, err := NewSSP(hco)
					Expect(err).To(HaveOccurred())
					Expect(ssp).To(BeNil())
				})

				It("Should reject if the CR list contain DIC template with the same name, and there are also common DIC templates", func() {
					err := os.Mkdir(dir, os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					defer func() { _ = os.RemoveAll(dir) }()
					destFile := path.Join(dir, "dataImportCronTemplates.yaml")

					err = commonTestUtils.CopyFile(destFile, path.Join(testFilesLocation, "dataImportCronTemplates.yaml"))
					Expect(err).ToNot(HaveOccurred())
					defer os.Remove(destFile)
					Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())

					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true

					Expect(dataImportCronTemplateHardCodedList).ToNot(BeEmpty())
					image3Modified := image3.DeepCopy()
					image3Modified.Name = image4.Name

					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{*image3Modified, image4}
					ssp, err := NewSSP(hco)
					Expect(err).To(HaveOccurred())
					Expect(ssp).To(BeNil())
				})

				It("Should reject if the CR list contain DIC template with the same name", func() {
					Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())

					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = false

					Expect(dataImportCronTemplateHardCodedList).To(BeEmpty())
					image3Modified := image3.DeepCopy()
					image3Modified.Name = image4.Name

					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{*image3Modified, image4}
					ssp, err := NewSSP(hco)
					Expect(err).To(HaveOccurred())
					Expect(ssp).To(BeNil())
				})

				It("should return a only the list from the HyperConverged CR, if the file is missing", func() {
					Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())
					Expect(dataImportCronTemplateHardCodedList).Should(BeEmpty())

					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = true
					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{image3, image4}
					ssp, err := NewSSP(hco)
					Expect(err).ToNot(HaveOccurred())

					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).ShouldNot(BeNil())
					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(HaveLen(2))
					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(ContainElements(image3, image4))
				})

				It("should not return the common templates, if feature gate is false", func() {
					err := os.Mkdir(dir, os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					defer func() { _ = os.RemoveAll(dir) }()
					destFile := path.Join(dir, "dataImportCronTemplates.yaml")

					err = commonTestUtils.CopyFile(destFile, path.Join(testFilesLocation, "dataImportCronTemplates.yaml"))
					Expect(err).ToNot(HaveOccurred())
					defer os.Remove(destFile)
					Expect(readDataImportCronTemplatesFromFile()).ToNot(HaveOccurred())

					hco := commonTestUtils.NewHco()
					hco.Spec.FeatureGates.EnableCommonBootImageImport = false
					hco.Spec.DataImportCronTemplates = []sspv1beta1.DataImportCronTemplate{image3, image4}
					ssp, err := NewSSP(hco)
					Expect(err).ToNot(HaveOccurred())

					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(HaveLen(2))
					Expect(ssp.Spec.CommonTemplates.DataImportCronTemplates).Should(ContainElements(image3, image4))
				})
			})

			Context("test applyDataImportSchedule", func() {
				It("should not set the schedule filed if missing from the status", func() {
					hco := commonTestUtils.NewHco()
					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{image1, image2}

					applyDataImportSchedule(hco)

					Expect(dataImportCronTemplateHardCodedList[0].Spec.Schedule).Should(Equal("1 */12 * * *"))
					Expect(dataImportCronTemplateHardCodedList[1].Spec.Schedule).Should(Equal("2 */12 * * *"))
				})

				It("should set the variable and the images, if the schedule is in the status field", func() {
					const schedule = "42 */1 * * *"
					hco := commonTestUtils.NewHco()
					hco.Status.DataImportSchedule = schedule

					dataImportCronTemplateHardCodedList = []sspv1beta1.DataImportCronTemplate{image1, image2}

					applyDataImportSchedule(hco)
					for _, image := range dataImportCronTemplateHardCodedList {
						Expect(image.Spec.Schedule).Should(Equal(schedule))
					}
				})
			})
		})
	})
})
