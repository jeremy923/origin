package bootstrappolicy_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util"

	"github.com/openshift/origin/pkg/api/v1"
	"github.com/openshift/origin/pkg/cmd/server/bootstrappolicy"

	// install all APIs
	_ "github.com/openshift/origin/pkg/api/install"
)

func TestOpenshiftRoles(t *testing.T) {
	roles := bootstrappolicy.GetBootstrapOpenshiftRoles("openshift")
	list := &api.List{}
	for i := range roles {
		list.Items = append(list.Items, &roles[i])
	}
	testObjects(t, list, "bootstrap_openshift_roles.yaml")
}

func TestBootstrapProjectRoleBindings(t *testing.T) {
	roleBindings := bootstrappolicy.GetBootstrapServiceAccountProjectRoleBindings("myproject")
	list := &api.List{}
	for i := range roleBindings {
		list.Items = append(list.Items, &roleBindings[i])
	}
	testObjects(t, list, "bootstrap_service_account_project_role_bindings.yaml")
}

func TestBootstrapClusterRoleBindings(t *testing.T) {
	roleBindings := bootstrappolicy.GetBootstrapClusterRoleBindings()
	list := &api.List{}
	for i := range roleBindings {
		list.Items = append(list.Items, &roleBindings[i])
	}
	testObjects(t, list, "bootstrap_cluster_role_bindings.yaml")
}

func TestBootstrapClusterRoles(t *testing.T) {
	roles := bootstrappolicy.GetBootstrapClusterRoles()
	list := &api.List{}
	for i := range roles {
		list.Items = append(list.Items, &roles[i])
	}
	testObjects(t, list, "bootstrap_cluster_roles.yaml")
}

func testObjects(t *testing.T, list *api.List, fixtureFilename string) {
	filename := filepath.Join("../../../../test/fixtures/bootstrappolicy", fixtureFilename)
	expectedYAML, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if err := runtime.EncodeList(api.Codecs.LegacyCodec(v1.SchemeGroupVersion), list.Items); err != nil {
		t.Fatal(err)
	}

	jsonData, err := runtime.Encode(api.Codecs.LegacyCodec(v1.SchemeGroupVersion), list)
	if err != nil {
		t.Fatal(err)
	}
	yamlData, err := yaml.JSONToYAML(jsonData)
	if err != nil {
		t.Fatal(err)
	}
	if string(yamlData) != string(expectedYAML) {
		fmt.Println("Bootstrap policy data does not match the test fixture in " + filename)
		fmt.Println("If the change is expected, update the fixture with this bootstrap policy data:")
		fmt.Println("-----------------------------------")
		fmt.Println(string(yamlData))
		fmt.Println("-----------------------------------")
		t.Errorf("Diff between bootstrap data and fixture data in %s:\n-------------\n%s", filename, util.StringDiff(string(yamlData), string(expectedYAML)))
	}
}
