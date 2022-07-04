package api_gateway

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/avast/retry-go"
	"github.com/kyma-project/kyma/tests/components/api-gateway/gateway-tests/pkg/client"
	"github.com/kyma-project/kyma/tests/components/api-gateway/gateway-tests/pkg/helpers"
	"github.com/kyma-project/kyma/tests/components/api-gateway/gateway-tests/pkg/jwt"
	"github.com/kyma-project/kyma/tests/components/api-gateway/gateway-tests/pkg/resource"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/cucumber/godog"

	"github.com/kyma-project/kyma/common/ingressgateway"

	"github.com/vrischmann/envconfig"

	"github.com/kyma-project/kyma/tests/components/api-gateway/gateway-tests/pkg/manifestprocessor"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func TestMain(m *testing.M) {

	if err := envconfig.Init(&conf); err != nil {
		log.Fatalf("Unable to setup config: %v", err)
	}

	if conf.IsMinikubeEnv {
		var err error
		log.Printf("Using dedicated ingress client")
		httpClient, err = ingressgateway.FromEnv().Client()
		if err != nil {
			log.Fatalf("Unable to initialize ingress gateway client: %v", err)
		}
	} else {
		log.Printf("Fallback to default http client")
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: conf.ClientTimeout,
		}
	}

	oauthClientID := generateRandomString(OauthClientIDLength)
	oauthClientSecret := generateRandomString(OauthClientSecretLength)
	namespace := fmt.Sprintf("api-gateway-test-%s", generateRandomString(6))
	randomSuffix6 := generateRandomString(6)
	oauthSecretName := fmt.Sprintf("api-gateway-tests-secret-%s", randomSuffix6)
	oauthClientName := fmt.Sprintf("api-gateway-tests-client-%s", randomSuffix6)
	log.Printf("Using namespace: %s\n", namespace)
	log.Printf("Using OAuth2Client with name: %s, secretName: %s\n", oauthClientName, oauthSecretName)

	oauth2Cfg = &clientcredentials.Config{
		ClientID:     oauthClientID,
		ClientSecret: oauthClientSecret,
		TokenURL:     fmt.Sprintf("%s/oauth2/token", conf.HydraAddr),
		Scopes:       []string{"read"},
		AuthStyle:    oauth2.AuthStyleInHeader,
	}

	jwtConf, err := jwt.LoadConfig(oauthClientID)
	if err != nil {
		log.Fatal(err)
	}

	jwtConfig = &jwtConf

	commonRetryOpts := []retry.Option{
		retry.Delay(time.Duration(conf.ReqDelay) * time.Second),
		retry.Attempts(conf.ReqTimeout / conf.ReqDelay),
		retry.DelayType(retry.FixedDelay),
	}

	helper = helpers.NewHelper(httpClient, commonRetryOpts)

	k8sClient := client.GetDynamicClient()
	resourceManager = &resource.Manager{RetryOptions: commonRetryOpts}

	batch = &resource.Batch{
		ResourceManager: resourceManager,
	}

	// create common resources for all scenarios
	globalCommonResources, err := manifestprocessor.ParseFromFileWithTemplate(globalCommonResourcesFile, manifestsDirectory, resourceSeparator, struct {
		Namespace         string
		OauthClientSecret string
		OauthClientID     string
		OauthSecretName   string
	}{
		Namespace:         namespace,
		OauthClientSecret: base64.StdEncoding.EncodeToString([]byte(oauthClientSecret)),
		OauthClientID:     base64.StdEncoding.EncodeToString([]byte(oauthClientID)),
		OauthSecretName:   oauthSecretName,
	})
	if err != nil {
		panic(err)
	}

	// delete test namespace if the previous test namespace persists
	nsResourceSchema, ns, name := resource.GetResourceSchemaAndNamespace(globalCommonResources[0])
	log.Printf("Delete test namespace, if exists: %s\n", name)
	resourceManager.DeleteResource(k8sClient, nsResourceSchema, ns, name)

	time.Sleep(time.Duration(conf.ReqDelay) * time.Second)

	log.Printf("Creating common tests resources")
	batch.CreateResources(k8sClient, globalCommonResources...)
	time.Sleep(time.Duration(conf.ReqDelay) * time.Second)

	hydraClientResource, err := manifestprocessor.ParseFromFileWithTemplate(hydraClientFile, manifestsDirectory, resourceSeparator, struct {
		Namespace       string
		OauthClientName string
		OauthSecretName string
	}{
		Namespace:       namespace,
		OauthClientName: oauthClientName,
		OauthSecretName: oauthSecretName,
	})
	if err != nil {
		panic(err)
	}
	log.Printf("Creating hydra client resources")
	batch.CreateResources(k8sClient, hydraClientResource...)
	// Let's wait a bit to register client in hydra
	time.Sleep(time.Duration(conf.ReqDelay) * time.Second)

	// defer deleting namespace (it will also delete all remaining resources in that namespace)
	defer func() {
		time.Sleep(time.Second * 3)
		resourceManager.DeleteResource(k8sClient, nsResourceSchema, ns, name)
	}()

	os.Exit(m.Run())
}

func TestApiGateway(t *testing.T) {
	prodOpts := goDogOpts
	prodOpts.Paths = []string{"features/unsecured_endpoint.feature"}

	suite := godog.TestSuite{
		Name:                "API-Gateway",
		ScenarioInitializer: InitializeApiGatewayTests,
		Options:             &prodOpts,
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
	if os.Getenv(exportResultVar) == "true" {
		generateHTMLReport()
	}
}

func InitializeApiGatewayTests(ctx *godog.ScenarioContext) {
	InitializeScenarioUnsecuredEndpoint(ctx)
}
