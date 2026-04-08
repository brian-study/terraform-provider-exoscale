package database_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	exoapi "github.com/exoscale/egoscale/v2/api"
	"github.com/exoscale/egoscale/v2/oapi"

	"github.com/exoscale/terraform-provider-exoscale/pkg/testutils"
)

type TemplateModelPg struct {
	ResourceName string

	Name string
	Plan string
	Zone string

	MaintenanceDow        string
	MaintenanceTime       string
	TerminationProtection bool

	AdminPassword     string
	AdminUsername     string
	BackupSchedule    string
	IpFilter          []string
	PgSettings        string
	PgbouncerSettings string
	PglookoutSettings string
	Version           string

	Integrations []TemplateModelPgIntegration
}

type TemplateModelPgIntegration struct {
	Type string
	// SourceService is rendered as-is into the terraform config, so it may be
	// either a quoted string literal ("foo") or a resource reference
	// (exoscale_dbaas.primary.name).
	SourceService string
}

type TemplateModelPgUser struct {
	ResourceName string

	Username string
	Service  string
	Zone     string
}

type TemplateModelPgDb struct {
	ResourceName string

	DatabaseName string
	Service      string
	Zone         string
}

func testResourcePg(t *testing.T) {
	t.Parallel()

	serviceTpl, err := template.ParseFiles("testdata/resource_pg.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	userTpl, err := template.ParseFiles("testdata/resource_user_pg.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	dbTpl, err := template.ParseFiles("testdata/resource_database_pg.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	serviceFullResourceName := "exoscale_dbaas.test"
	serviceDataBase := TemplateModelPg{
		ResourceName:          "test",
		Name:                  acctest.RandomWithPrefix(testutils.Prefix),
		Plan:                  "hobbyist-2",
		Zone:                  testutils.TestZoneName,
		TerminationProtection: false,
		Version:               "15",
	}

	userFullResourceName := "exoscale_dbaas_pg_user.test_user"
	userDataBase := TemplateModelPgUser{
		ResourceName: "test_user",
		Username:     "foo",
		Zone:         serviceDataBase.Zone,
		Service:      fmt.Sprintf("%s.name", serviceFullResourceName),
	}

	dbFullResourceName := "exoscale_dbaas_pg_database.test_database"
	dbDataBase := TemplateModelPgDb{
		ResourceName: "test_database",
		DatabaseName: "foo_db",
		Zone:         serviceDataBase.Zone,
		Service:      fmt.Sprintf("%s.name", serviceFullResourceName),
	}

	serviceDataCreate := serviceDataBase
	serviceDataCreate.MaintenanceDow = "monday"
	serviceDataCreate.MaintenanceTime = "01:23:00"
	serviceDataCreate.BackupSchedule = "01:23"
	serviceDataCreate.IpFilter = []string{"1.2.3.4/32"}
	serviceDataCreate.PgSettings = strconv.Quote(`{"timezone":"Europe/Zurich"}`)
	serviceDataCreate.PgbouncerSettings = strconv.Quote(`{"min_pool_size":10}`)

	userDataCreate := userDataBase
	dbDataCreate := dbDataBase

	buf := &bytes.Buffer{}
	err = serviceTpl.Execute(buf, &serviceDataCreate)
	if err != nil {
		t.Fatal(err)
	}
	err = userTpl.Execute(buf, &userDataCreate)
	if err != nil {
		t.Fatal(err)
	}
	err = dbTpl.Execute(buf, &dbDataCreate)
	if err != nil {
		t.Fatal(err)
	}
	configCreate := buf.String()

	serviceDataUpdate := serviceDataBase
	serviceDataUpdate.MaintenanceDow = "tuesday"
	serviceDataUpdate.MaintenanceTime = "02:34:00"
	serviceDataUpdate.BackupSchedule = "23:45"
	serviceDataUpdate.IpFilter = nil
	serviceDataUpdate.PgSettings = strconv.Quote(`{"max_worker_processes":10,"timezone":"Europe/Zurich"}`)
	serviceDataUpdate.PgbouncerSettings = strconv.Quote(`{"autodb_pool_size":5,"min_pool_size":10}`)
	serviceDataUpdate.PglookoutSettings = strconv.Quote(`{"max_failover_replication_time_lag":30}`)

	userDataUpdate := userDataBase
	userDataUpdate.Username = "bar"

	dbDataUpdate := dbDataBase
	dbDataUpdate.DatabaseName = "bar_db"

	buf = &bytes.Buffer{}
	err = serviceTpl.Execute(buf, &serviceDataUpdate)
	if err != nil {
		t.Fatal(err)
	}
	err = userTpl.Execute(buf, &userDataUpdate)
	if err != nil {
		t.Fatal(err)
	}
	err = dbTpl.Execute(buf, &dbDataUpdate)
	if err != nil {
		t.Fatal(err)
	}
	configUpdate := buf.String()

	serviceDataScale := serviceDataUpdate
	serviceDataScale.Plan = "startup-4"

	buf = &bytes.Buffer{}
	err = serviceTpl.Execute(buf, &serviceDataScale)
	if err != nil {
		t.Fatal(err)
	}
	err = userTpl.Execute(buf, &userDataUpdate)
	if err != nil {
		t.Fatal(err)
	}
	err = dbTpl.Execute(buf, &dbDataUpdate)
	if err != nil {
		t.Fatal(err)
	}
	configScale := buf.String()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutils.AccPreCheck(t) },
		CheckDestroy:             CheckServiceDestroy("pg", serviceDataBase.Name),
		ProtoV6ProviderFactories: testutils.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Create
				Config: configCreate,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Service
					resource.TestCheckResourceAttrSet(serviceFullResourceName, "created_at"),
					resource.TestCheckResourceAttrSet(serviceFullResourceName, "disk_size"),
					resource.TestCheckResourceAttrSet(serviceFullResourceName, "node_cpus"),
					resource.TestCheckResourceAttrSet(serviceFullResourceName, "node_memory"),
					resource.TestCheckResourceAttrSet(serviceFullResourceName, "nodes"),
					resource.TestCheckResourceAttrSet(serviceFullResourceName, "ca_certificate"),
					resource.TestCheckResourceAttrSet(serviceFullResourceName, "updated_at"),
					func(s *terraform.State) error {
						err := CheckExistsPg(serviceDataBase.Name, &serviceDataCreate)
						if err != nil {
							return err
						}

						return nil
					},
					// User
					resource.TestCheckResourceAttrSet(userFullResourceName, "password"),
					resource.TestCheckResourceAttrSet(userFullResourceName, "type"),
					func(s *terraform.State) error {
						err := CheckExistsPgUser(serviceDataBase.Name, userDataBase.Username, &userDataCreate)
						if err != nil {
							return err
						}

						return nil
					},

					// Database
					func(s *terraform.State) error {
						err := CheckExistsPgDatabase(serviceDataBase.Name, dbDataCreate.DatabaseName, &dbDataCreate)
						if err != nil {
							return err
						}

						return nil
					},
				),
			},
			{
				// Update
				Config: configUpdate,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Service
					func(s *terraform.State) error {
						err := CheckExistsPg(serviceDataBase.Name, &serviceDataUpdate)
						if err != nil {
							return err
						}

						return nil
					},

					// User
					func(s *terraform.State) error {
						// Check the old user was deleted
						err := CheckExistsPgUser(serviceDataBase.Name, userDataBase.Username, &userDataUpdate)
						if err == nil {
							return fmt.Errorf("expected to not find user %s", userDataBase.Username)
						}

						// Check the new user exists
						err = CheckExistsPgUser(serviceDataBase.Name, userDataUpdate.Username, &userDataUpdate)
						if err != nil {
							return err
						}

						return nil
					},

					// Database
					func(s *terraform.State) error {
						// Check the old database was deleted
						err := CheckExistsPgDatabase(serviceDataBase.Name, dbDataBase.DatabaseName, &dbDataUpdate)
						if err == nil {
							return fmt.Errorf("expected to not find database %s", dbDataBase.DatabaseName)
						}

						// Check the new user exists
						err = CheckExistsPgDatabase(serviceDataBase.Name, dbDataUpdate.DatabaseName, &dbDataUpdate)
						if err != nil {
							return err
						}
						return nil
					},
				),
			},
			{
				// Scale
				Config: configScale,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Service
					func(s *terraform.State) error {
						err := CheckExistsPg(serviceDataBase.Name, &serviceDataScale)
						if err != nil {
							return err
						}

						return nil
					},
				),
			},
			{
				// Import
				ResourceName: serviceFullResourceName,
				ImportStateIdFunc: func() resource.ImportStateIdFunc {
					return func(*terraform.State) (string, error) {
						return fmt.Sprintf("%s@%s", serviceDataBase.Name, serviceDataBase.Zone), nil
					}
				}(),
				ImportState: true,
				// NOTE: ImportStateVerify doesn't work when there are optional attributes.
				//ImportStateVerify: true
			},
			{
				ResourceName: userFullResourceName,
				ImportStateIdFunc: func() resource.ImportStateIdFunc {
					return func(*terraform.State) (string, error) {
						return fmt.Sprintf("%s/%s@%s", serviceDataBase.Name, userDataUpdate.Username, userDataBase.Zone), nil
					}
				}(),
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName: dbFullResourceName,
				ImportStateIdFunc: func() resource.ImportStateIdFunc {
					return func(*terraform.State) (string, error) {
						return fmt.Sprintf("%s/%s@%s", serviceDataBase.Name, dbDataUpdate.DatabaseName, dbDataBase.Zone), nil
					}
				}(),
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func CheckExistsPg(name string, data *TemplateModelPg) error {
	client, err := testutils.APIClient()
	if err != nil {
		return err
	}

	ctx := exoapi.WithEndpoint(context.Background(), exoapi.NewReqEndpoint(testutils.TestEnvironment(), testutils.TestZoneName))

	res, err := client.GetDbaasServicePgWithResponse(ctx, oapi.DbaasServiceName(name))
	if err != nil {
		return err
	}
	if res.StatusCode() != http.StatusOK {
		return fmt.Errorf("API request error: unexpected status %s", res.Status())
	}
	service := res.JSON200

	if data.Plan != service.Plan {
		return fmt.Errorf("plan: expected %q, got %q", data.Plan, service.Plan)
	}

	if v := fmt.Sprintf("%02d:%02d", *service.BackupSchedule.BackupHour, *service.BackupSchedule.BackupMinute); data.BackupSchedule != v {
		return fmt.Errorf("backup_schedule: expected %q, got %q", data.BackupSchedule, v)
	}

	if *service.TerminationProtection != false {
		return fmt.Errorf("termination_protection: expected false, got true")
	}

	if !cmp.Equal(data.IpFilter, *service.IpFilter, cmpopts.EquateEmpty()) {
		return fmt.Errorf("pg.ip_filter: expected %q, got %q", data.IpFilter, *service.IpFilter)
	}

	if v := string(service.Maintenance.Dow); data.MaintenanceDow != v {
		return fmt.Errorf("pg.maintenance_dow: expected %q, got %q", data.MaintenanceDow, v)
	}

	if data.MaintenanceTime != service.Maintenance.Time {
		return fmt.Errorf("pg.maintenance_time: expected %q, got %q", data.MaintenanceTime, service.Maintenance.Time)
	}

	serviceMajVersion := strings.Split(*service.Version, ".")[0]

	if data.Version != serviceMajVersion {
		return fmt.Errorf("pg.version: expected %q, got %q", data.Version, serviceMajVersion)
	}

	//  NOTE: Due to default values setup by Aiven, we won't validate settings.

	return nil
}

func CheckExistsPgUser(service, username string, data *TemplateModelPgUser) error {

	client, err := testutils.APIClient()
	if err != nil {
		return err
	}

	ctx := exoapi.WithEndpoint(context.Background(), exoapi.NewReqEndpoint(testutils.TestEnvironment(), testutils.TestZoneName))
	serviceUsernames := make([]string, 0)

	ch := make(chan any, 1)
	go func() {
		time.Sleep(60 * time.Second)
		ch <- "timeout!"
	}()
	for len(ch) == 0 {
		res, err := client.GetDbaasServicePgWithResponse(ctx, oapi.DbaasServiceName(service))
		if err != nil {
			return err
		}
		if res.StatusCode() != http.StatusOK {
			return fmt.Errorf("API request error: unexpected status %s", res.Status())
		}
		svc := res.JSON200

		if svc.Users != nil {
			for _, u := range *svc.Users {
				serviceUsernames = append(serviceUsernames, u.Username)
				if u.Username == username {
					return nil
				}
			}
		}
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("could not find user %s for service %s, found %v", username, service, serviceUsernames)
}

func CheckExistsPgDatabase(service, databaseName string, data *TemplateModelPgDb) error {

	client, err := testutils.APIClient()
	if err != nil {
		return err
	}

	ctx := exoapi.WithEndpoint(context.Background(), exoapi.NewReqEndpoint(testutils.TestEnvironment(), testutils.TestZoneName))
	serviceDbs := make([]string, 0)

	ch := make(chan any, 1)
	go func() {
		time.Sleep(60 * time.Second)
		ch <- "timeout!"
	}()
	for len(ch) == 0 {

		res, err := client.GetDbaasServicePgWithResponse(ctx, oapi.DbaasServiceName(service))
		if err != nil {
			return err
		}
		if res.StatusCode() != http.StatusOK {
			return fmt.Errorf("API request error: unexpected status %s", res.Status())
		}
		svc := res.JSON200

		if svc.Databases != nil {
			for _, db := range *svc.Databases {
				serviceDbs = append(serviceDbs, string(db))
				if string(db) == databaseName {
					return nil
				}
			}
		}
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("could not find database %s for service %s, found %v", databaseName, service, serviceDbs)
}

// testResourcePgIntegrations exercises creating a PG service that is declared
// as a read replica of another PG service via the `integrations` attribute,
// and verifies that modifying the integration triggers a full replace of the
// replica resource (since integrations cannot be updated in place).
func testResourcePgIntegrations(t *testing.T) {
	t.Parallel()

	serviceTpl, err := template.ParseFiles("testdata/resource_pg.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	// Plan choice note: Exoscale DBaaS read replicas require the primary
	// service to be on a Business or Premium plan (Hobbyist and Startup
	// are not sufficient). The replica itself can run on any Startup or
	// larger plan. `business-4` / `startup-4` is the cheapest authorized
	// combination that actually supports the read_replica integration.
	primary := TemplateModelPg{
		ResourceName:          "primary",
		Name:                  acctest.RandomWithPrefix(testutils.Prefix),
		Plan:                  "business-4",
		Zone:                  testutils.TestZoneName,
		TerminationProtection: false,
		Version:               "15",
	}
	replica := TemplateModelPg{
		ResourceName:          "replica",
		Name:                  acctest.RandomWithPrefix(testutils.Prefix),
		Plan:                  "startup-4",
		Zone:                  testutils.TestZoneName,
		TerminationProtection: false,
		Version:               "15",
		Integrations: []TemplateModelPgIntegration{
			{
				Type:          "read_replica",
				SourceService: "exoscale_dbaas.primary.name",
			},
		},
	}

	renderPgConfig := func(services ...TemplateModelPg) string {
		t.Helper()
		buf := &bytes.Buffer{}
		for _, s := range services {
			if err := serviceTpl.Execute(buf, &s); err != nil {
				t.Fatal(err)
			}
		}
		return buf.String()
	}

	configCreate := renderPgConfig(primary, replica)

	// Second variant: add a second primary and swap the replica's
	// source_service to point at it. Changing source_service must force the
	// replica to be replaced (destroy+create).
	primary2 := TemplateModelPg{
		ResourceName:          "primary2",
		Name:                  acctest.RandomWithPrefix(testutils.Prefix),
		Plan:                  "business-4",
		Zone:                  testutils.TestZoneName,
		TerminationProtection: false,
		Version:               "15",
	}
	replicaSwapped := replica
	// Rotate the replica name on the swap step so the destroy+create
	// cycle does not hit a 409 on eventual-consistency in the DBaaS API.
	replicaSwapped.Name = acctest.RandomWithPrefix(testutils.Prefix)
	replicaSwapped.Integrations = []TemplateModelPgIntegration{
		{
			Type:          "read_replica",
			SourceService: "exoscale_dbaas.primary2.name",
		},
	}
	configSwap := renderPgConfig(primary, primary2, replicaSwapped)

	primaryFullResourceName := "exoscale_dbaas.primary"
	primary2FullResourceName := "exoscale_dbaas.primary2"
	replicaFullResourceName := "exoscale_dbaas.replica"

	integrationContains := func(primaryResource string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			primaryRes, ok := s.RootModule().Resources[primaryResource]
			if !ok {
				return fmt.Errorf("resource %s not found in state", primaryResource)
			}
			primaryName := primaryRes.Primary.Attributes["name"]
			if primaryName == "" {
				return fmt.Errorf("resource %s has no `name` attribute in state", primaryResource)
			}
			return resource.TestCheckTypeSetElemNestedAttrs(
				replicaFullResourceName,
				"pg.integrations.*",
				map[string]string{
					"type":           "read_replica",
					"source_service": primaryName,
				},
			)(s)
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() { testutils.AccPreCheck(t) },
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			CheckServiceDestroy("pg", primary.Name),
			CheckServiceDestroy("pg", primary2.Name),
			CheckServiceDestroy("pg", replica.Name),
			CheckServiceDestroy("pg", replicaSwapped.Name),
		),
		ProtoV6ProviderFactories: testutils.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configCreate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(primaryFullResourceName, "created_at"),
					resource.TestCheckResourceAttrSet(replicaFullResourceName, "created_at"),
					resource.TestCheckResourceAttr(replicaFullResourceName, "pg.integrations.#", "1"),
					integrationContains(primaryFullResourceName),
					func(s *terraform.State) error {
						return CheckPgIntegrationExists(replica.Name, primary.Name, "read_replica")
					},
				),
			},
			{
				// Verify the import round-trip of the integrations
				// attribute — the Read path's dest-filter is the
				// most fragile part of the PR, so we exercise it
				// explicitly. ImportStateVerify (the usual helper) is
				// not used because other computed-or-optional pg
				// attributes (admin_password, settings, ...) don't
				// round-trip. Instead we drive a custom
				// ImportStateCheck that inspects the imported state
				// and asserts only what this PR cares about:
				//   - pg.integrations.# == "1"
				//   - exactly one set element matches the expected
				//     {type, source_service} pair
				// This gives the panel-review-requested
				// "import actually verifies integrations" signal
				// without dragging in unrelated drift.
				ResourceName: replicaFullResourceName,
				ImportStateIdFunc: func() resource.ImportStateIdFunc {
					return func(*terraform.State) (string, error) {
						return fmt.Sprintf("%s@%s", replica.Name, replica.Zone), nil
					}
				}(),
				ImportState: true,
				ImportStateCheck: assertImportedIntegrations(
					"pg",
					"read_replica",
					&primary.Name,
					1,
				),
			},
			{
				// Swapping source_service must force the replica to be
				// replaced — verify via plancheck first, then assert the
				// new integration is in place after apply.
				Config: configSwap,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(replicaFullResourceName, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(primary2FullResourceName, "created_at"),
					resource.TestCheckResourceAttr(replicaFullResourceName, "pg.integrations.#", "1"),
					integrationContains(primary2FullResourceName),
					func(s *terraform.State) error {
						return CheckPgIntegrationExists(replicaSwapped.Name, primary2.Name, "read_replica")
					},
				),
			},
		},
	})
}

// CheckPgIntegrationExists verifies that the DBaaS API reports an integration
// of the given type between source and dest (dest being the service currently
// declared with the integration in its spec).
func CheckPgIntegrationExists(dest, source, integrationType string) error {
	client, err := testutils.APIClient()
	if err != nil {
		return err
	}

	// terraform-plugin-testing TestCheckFunc has no context, so we make a fresh one.
	ctx := exoapi.WithEndpoint(context.Background(), exoapi.NewReqEndpoint(testutils.TestEnvironment(), testutils.TestZoneName))

	res, err := client.GetDbaasServicePgWithResponse(ctx, oapi.DbaasServiceName(dest))
	if err != nil {
		return err
	}
	if res.StatusCode() != http.StatusOK {
		return fmt.Errorf("API request error: unexpected status %s", res.Status())
	}
	service := res.JSON200

	if service.Integrations == nil {
		return fmt.Errorf("no integrations reported for service %q", dest)
	}
	for _, integration := range *service.Integrations {
		if integration.Dest == nil || integration.Source == nil || integration.Type == nil {
			continue
		}
		if *integration.Dest == dest && *integration.Source == source && *integration.Type == integrationType {
			return nil
		}
	}
	return fmt.Errorf("integration %q from %q to %q not found on service %q", integrationType, source, dest, dest)
}

// assertImportedIntegrations returns an ImportStateCheck callback that
// inspects the imported state and asserts the nested `integrations`
// attribute round-tripped with the expected count and at least one
// element matching (type, *expectedSourceName). Works for both `pg.`
// and `mysql.` top-level blocks — pass the dbType accordingly.
//
// Set nested attributes in flatmap state use hash-keyed element paths
// (e.g. `pg.integrations.1234567890.type`), so we scan the Primary
// attributes for the count key and any matching element.
//
// expectedSourceName is a pointer because the source name is not known
// until the primary resource has been applied — the caller typically
// passes &primary.Name, and Go closes over it; the pointer is
// dereferenced lazily at import-check time.
func assertImportedIntegrations(dbType, expectedType string, expectedSourceName *string, expectedCount int) resource.ImportStateCheckFunc {
	return func(states []*terraform.InstanceState) error {
		if len(states) == 0 {
			return fmt.Errorf("no imported instance states returned")
		}
		state := states[0]

		countKey := fmt.Sprintf("%s.integrations.#", dbType)
		countStr, ok := state.Attributes[countKey]
		if !ok {
			return fmt.Errorf("imported state has no %q attribute", countKey)
		}
		if countStr != fmt.Sprintf("%d", expectedCount) {
			return fmt.Errorf("imported state %s = %q, want %q", countKey, countStr, fmt.Sprintf("%d", expectedCount))
		}

		// Walk all hashed-set element paths, grouping by hash.
		prefix := fmt.Sprintf("%s.integrations.", dbType)
		seen := map[string]map[string]string{}
		for k, v := range state.Attributes {
			if !strings.HasPrefix(k, prefix) || k == countKey {
				continue
			}
			rest := strings.TrimPrefix(k, prefix)
			// rest looks like "<hash>.type" or "<hash>.source_service"
			parts := strings.SplitN(rest, ".", 2)
			if len(parts) != 2 {
				continue
			}
			hash, field := parts[0], parts[1]
			if _, ok := seen[hash]; !ok {
				seen[hash] = map[string]string{}
			}
			seen[hash][field] = v
		}

		want := *expectedSourceName
		for hash, fields := range seen {
			if fields["type"] == expectedType && fields["source_service"] == want {
				return nil
			}
			_ = hash
		}
		return fmt.Errorf(
			"imported state has no %s.integrations element with type=%q source_service=%q; got %d element(s): %+v",
			dbType, expectedType, want, len(seen), seen,
		)
	}
}

// testResourcePgIntegrationsValidators exercises the attribute-level
// validators on the `integrations` set: SizeAtLeast(1), OneOf("read_replica")
// on `type`, and the custom self-source rejector. All steps are plan-only
// with ExpectError regexps — no Exoscale resources are created, so the
// test has zero API cost. Still routed through TestDatabase so it runs
// alongside the existing AccPreCheck credentials gate.
func testResourcePgIntegrationsValidators(t *testing.T) {
	t.Parallel()

	serviceName := acctest.RandomWithPrefix(testutils.Prefix)

	// Minimal valid skeleton except for the integrations block we vary
	// per step. The config references a non-existent primary service by
	// name so terraform's dependency graph resolves without requiring a
	// second resource — we only need validate/plan to run.
	//
	// `zone` is a real authorized zone so the zone OneOf validator passes;
	// the resource never reaches the Create RPC because the integrations
	// validator fires first.
	configWithIntegrations := func(name string, integrationsBlock string) string {
		return fmt.Sprintf(`
resource "exoscale_dbaas" "target" {
  name = %q
  type = "pg"
  plan = "business-4"
  zone = %q
  pg = {
    version = "15"
    %s
  }
}
`, name, testutils.TestZoneName, integrationsBlock)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutils.AccPreCheck(t) },
		ProtoV6ProviderFactories: testutils.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// setvalidator.SizeAtLeast(1) — empty set must be rejected
				// at plan time so users don't hit a perpetual diff.
				Config:   configWithIntegrations(serviceName, `integrations = []`),
				PlanOnly: true,
				ExpectError: regexp.MustCompile(
					`(?s)set must contain at least 1 elements`,
				),
			},
			{
				// stringvalidator.OneOf("read_replica") on `type`.
				Config: configWithIntegrations(serviceName, `integrations = [{
      type           = "logs"
      source_service = "some-other-service"
    }]`),
				PlanOnly: true,
				ExpectError: regexp.MustCompile(
					`(?s)value must be one of.*read_replica.*got: "logs"`,
				),
			},
			{
				// Custom integrationsSelfSource validator — refuse an
				// integration whose source_service equals the resource's
				// own name. Catches copy-paste mistakes at plan time
				// instead of after a slow failed API call.
				Config: configWithIntegrations(serviceName, fmt.Sprintf(`integrations = [{
      type           = "read_replica"
      source_service = %q
    }]`, serviceName)),
				PlanOnly: true,
				ExpectError: regexp.MustCompile(
					`(?s)Invalid integration source`,
				),
			},
		},
	})
}
