package v1beta3_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/emqx/emqx-operator/apis/apps/v1beta3"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("EMQX Broker", func() {
	Context("Check EMQX Broker", func() {
		v1beta3EmqxBroker := &v1beta3.EmqxBroker{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "broker",
				Namespace: "default",
				Labels: map[string]string{
					"foo": "bar",
				},
				Annotations: map[string]string{
					"foo": "bar",
				},
			},
			Spec: v1beta3.EmqxBrokerSpec{
				EmqxTemplate: v1beta3.EmqxBrokerTemplate{
					Image: "emqx/emqx:4.4.3",
				},
			},
		}
		BeforeEach(func() {
			Expect(k8sClient.Create(context.Background(), v1beta3EmqxBroker)).Should(Succeed())
		})
		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), v1beta3EmqxBroker)).Should(Succeed())
		})
		It("Check defaulting", func() {
			checkDefaulting(v1beta3EmqxBroker)
		})
	})
})

var _ = Describe("EMQX Enterprise", func() {
	Context("Check EMQX Enterprise", func() {
		v1beta3EmqxEnterprise := &v1beta3.EmqxEnterprise{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "enterprise",
				Namespace: "default",
				Labels: map[string]string{
					"foo": "bar",
				},
				Annotations: map[string]string{
					"foo": "bar",
				},
			},
			Spec: v1beta3.EmqxEnterpriseSpec{
				EmqxTemplate: v1beta3.EmqxEnterpriseTemplate{
					Image: "emqx/emqx-ee:4.4.3",
				},
			},
		}
		BeforeEach(func() {
			Expect(k8sClient.Create(context.Background(), v1beta3EmqxEnterprise)).Should(Succeed())
		})
		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), v1beta3EmqxEnterprise)).Should(Succeed())
		})
		It("Check defaulting", func() {
			checkDefaulting(v1beta3EmqxEnterprise)
		})
	})
})

func checkDefaulting(emqx v1beta3.Emqx) {
	Eventually(func() error {
		err := k8sClient.Get(
			context.TODO(),
			types.NamespacedName{
				Name:      emqx.GetName(),
				Namespace: emqx.GetNamespace(),
			},
			emqx,
		)
		return err
	}, timeout, interval).Should(Succeed())

	Expect(emqx.GetLabels()).Should(HaveKeyWithValue("foo", "bar"))
	Expect(emqx.GetLabels()).Should(HaveKeyWithValue("apps.emqx.io/managed-by", "emqx-operator"))
	Expect(emqx.GetLabels()).Should(HaveKeyWithValue("apps.emqx.io/instance", emqx.GetName()))

	replicas := int32(3)
	Expect(emqx.GetReplicas()).Should(Equal(&replicas))

	Expect(emqx.GetServiceTemplate().Name).Should(Equal(emqx.GetName()))
	Expect(emqx.GetServiceTemplate().Namespace).Should(Equal(emqx.GetNamespace()))
	Expect(emqx.GetServiceTemplate().Labels).Should(HaveKeyWithValue("foo", "bar"))
	Expect(emqx.GetServiceTemplate().Annotations).Should(HaveKeyWithValue("foo", "bar"))
	Expect(emqx.GetServiceTemplate().Spec.Selector).Should(HaveKeyWithValue("foo", "bar"))

	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("log.to", "both"))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("name", emqx.GetName()))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("listener.tcp.external", "1883"))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("listener.ssl.external", "8883"))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("listener.ws.external", "8083"))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("listener.wss.external", "8084"))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("cluster.discovery", "dns"))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("cluster.dns.type", "srv"))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("cluster.dns.app", emqx.GetName()))
	Expect(emqx.GetEmqxConfig()).Should(HaveKeyWithValue("cluster.dns.name", fmt.Sprintf("%s-headless.%s.svc.cluster.local", emqx.GetName(), emqx.GetNamespace())))

}