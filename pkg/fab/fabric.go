package fab

import (
	"bytes"
	_ "embed"
	"fmt"
	"log/slog"
	"slices"

	helm "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	agentapi "go.githedgehog.com/fabric/api/agent/v1alpha2"
	"go.githedgehog.com/fabric/pkg/wiring"
	"go.githedgehog.com/fabricator/pkg/fab/cnc"
)

//go:embed fabric_values.tmpl.yaml
var fabricValuesTemplate string

//go:embed fabric_config.tmpl.yaml
var fabricConfigTemplate string

//go:embed fabric_dhcp_server_values.tmpl.yaml
var fabricDHCPServerTemplate string

type Fabric struct {
	Ref                      cnc.Ref `json:"ref,omitempty"`
	FabricApiChartRef        cnc.Ref `json:"fabricApiChartRef,omitempty"`
	FabricChartRef           cnc.Ref `json:"fabricChartRef,omitempty"`
	FabricImageRef           cnc.Ref `json:"fabricImageRef,omitempty"`
	AgentRef                 cnc.Ref `json:"agentRef,omitempty"`
	CtlRef                   cnc.Ref `json:"ctlRef,omitempty"`
	FabricDHCPServerRef      cnc.Ref `json:"dhcpServerRef,omitempty"`
	FabricDHCPServerChartRef cnc.Ref `json:"dhcpServerChartRef,omitempty"`
	VPCBackend               string  `json:"vpcBackend,omitempty"`
	SNATAllowed              bool    `json:"snatAllowed,omitempty"`
}

var _ cnc.Component = (*Fabric)(nil)

func (cfg *Fabric) Name() string {
	return "fabric"
}

func (cfg *Fabric) IsEnabled(preset cnc.Preset) bool {
	return true
}

func (cfg *Fabric) Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:        "vpc-backend",
			Usage:       "VPC backend (VRF-based or ACL-based VPC implementation), using incorrect value may result in undefined behavior",
			EnvVars:     []string{"HHFAB_FABRIC_VPC_BACKEND"},
			Destination: &cfg.VPCBackend,
		},
		&cli.BoolFlag{
			Name:        "snat-allowed",
			Usage:       "Allow SNAT for VPCs, using SNAT may result in undefined behavior",
			EnvVars:     []string{"HHFAB_FABRIC_SNAT_ALLOWED"},
			Destination: &cfg.SNATAllowed,
		},
	}
}

func (cfg *Fabric) Hydrate(preset cnc.Preset) error {
	cfg.Ref = cfg.Ref.Fallback(REF_FABRIC_VERSION)
	cfg.FabricApiChartRef = cfg.FabricApiChartRef.Fallback(REF_FABRIC_API_CHART)
	cfg.FabricChartRef = cfg.FabricChartRef.Fallback(REF_FABRIC_CHART)
	cfg.FabricImageRef = cfg.FabricImageRef.Fallback(REF_FABRIC_IMAGE)
	cfg.AgentRef = cfg.AgentRef.Fallback(REF_FABRIC_AGENT)
	cfg.CtlRef = cfg.CtlRef.Fallback(REF_FABRIC_CTL)
	cfg.FabricDHCPServerRef = cfg.FabricDHCPServerRef.Fallback(REF_FABRIC_DHCP_SERVER)
	cfg.FabricDHCPServerChartRef = cfg.FabricDHCPServerChartRef.Fallback(REF_FABRIC_DHCP_SERVER_CHART)

	if cfg.VPCBackend == "" {
		if preset == PRESET_BM {
			cfg.VPCBackend = "acl"
		} else if preset == PRESET_VLAB {
			cfg.VPCBackend = "vrf"
		}
	}
	if !slices.Contains(agentapi.VPCBackendValues, agentapi.VPCBackend(cfg.VPCBackend)) {
		return errors.Errorf("invalid VPC backend %q", cfg.VPCBackend)
	}

	return nil
}

func (cfg *Fabric) Build(basedir string, preset cnc.Preset, get cnc.GetComponent, wiring *wiring.Data, run cnc.AddBuildOp, install cnc.AddRunOp) error {
	cfg.FabricApiChartRef = cfg.FabricApiChartRef.Fallback(cfg.Ref, BaseConfig(get).Source)
	cfg.FabricChartRef = cfg.FabricChartRef.Fallback(cfg.Ref, BaseConfig(get).Source)
	cfg.FabricImageRef = cfg.FabricImageRef.Fallback(cfg.Ref, BaseConfig(get).Source)
	cfg.AgentRef = cfg.AgentRef.Fallback(cfg.Ref, BaseConfig(get).Source)
	cfg.CtlRef = cfg.CtlRef.Fallback(cfg.Ref, BaseConfig(get).Source)
	cfg.FabricDHCPServerRef = cfg.FabricDHCPServerRef.Fallback(cfg.Ref, BaseConfig(get).Source)
	cfg.FabricDHCPServerChartRef = cfg.FabricDHCPServerChartRef.Fallback(cfg.Ref, BaseConfig(get).Source)

	target := BaseConfig(get).Target
	targetInCluster := BaseConfig(get).TargetInCluster

	if preset == PRESET_BM && cfg.VPCBackend != "acl" {
		slog.Warn("VPC backend is not ACL with BM preset, this may result in undefined behavior")
	} else if preset == PRESET_VLAB && cfg.VPCBackend != "vrf" {
		slog.Warn("VPC backend is not VRF with VLAB preset, this may result in undefined behavior")
	}
	if cfg.SNATAllowed {
		slog.Warn("SNAT is allowed, this may result in undefined behavior")
	}

	wiringData := &bytes.Buffer{}
	err := wiring.Write(wiringData) // TODO extract to lib
	if err != nil {
		return errors.Wrap(err, "error writing wiring data")
	}

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-api-chart",
		&cnc.SyncOCI{
			Ref:    cfg.FabricApiChartRef,
			Target: target,
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-image",
		&cnc.SyncOCI{
			Ref:    cfg.FabricImageRef,
			Target: target,
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-chart",
		&cnc.SyncOCI{
			Ref:    cfg.FabricChartRef,
			Target: target,
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-agent-seeder",
		&cnc.SyncOCI{
			Ref:    cfg.AgentRef,
			Target: target.Fallback(cnc.Ref{Name: "fabric/agent/x86_64", Tag: "latest"}),
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-agent",
		&cnc.SyncOCI{
			Ref:    cfg.AgentRef,
			Target: target,
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-dhcp-server-image",
		&cnc.SyncOCI{
			Ref:    cfg.FabricDHCPServerRef,
			Target: target,
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-dhcp-server-chart",
		&cnc.SyncOCI{
			Ref:    cfg.FabricDHCPServerChartRef,
			Target: target,
		})

	users := []agentapi.UserCreds{}
	slog.Info("Base config", "dev", BaseConfig(get).Dev)
	if BaseConfig(get).Dev {
		users = append(users, DEV_SONIC_USERS...)
		slog.Info("Adding dev users", "users", users)
		for idx := range users {
			users[idx].SSHKeys = append(users[idx].SSHKeys, BaseConfig(get).AuthorizedKeys...)
			slog.Info("Adding dev ssh keys to user", "user", users[idx])
		}
	}

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-install",
		&cnc.FileGenerate{
			File: cnc.File{
				Name:          "fabric-install.yaml",
				InstallTarget: "/var/lib/rancher/k3s/server/manifests",
				InstallName:   "hh-fabric-install.yaml",
			},
			Content: cnc.FromKubeObjects(
				cnc.KubeHelmChart("fabric-api", "default", helm.HelmChartSpec{
					TargetNamespace: "default",
					Chart:           "oci://" + targetInCluster.Fallback(cfg.FabricApiChartRef).RepoName(),
					Version:         cfg.FabricApiChartRef.Tag,
					RepoCA:          ZotConfig(get).TLS.CA.Cert,
					FailurePolicy:   "abort", // very important not to re-install crd charts
				}, cnc.FromValue("")),
				cnc.KubeHelmChart("fabric", "default", helm.HelmChartSpec{
					TargetNamespace: "default",
					Chart:           "oci://" + targetInCluster.Fallback(cfg.FabricChartRef).RepoName(),
					Version:         cfg.FabricChartRef.Tag,
					RepoCA:          ZotConfig(get).TLS.CA.Cert,
				}, cnc.FromTemplate(fabricValuesTemplate,
					"ref", target.Fallback(cfg.FabricImageRef),
					"proxyRef", target.Fallback(MiscConfig(get).RBACProxyImageRef),
				)),
				cnc.KubeConfigMap("fabric-config", "default",
					"config.yaml",
					cnc.FromTemplate(fabricConfigTemplate,
						"apiServer", fmt.Sprintf("%s:%d", CONTROL_VIP, K3S_API_PORT),
						"controlVIP", CONTROL_VIP+CONTROL_VIP_MASK,
						"vpcVLANMin", VPC_VLAN_MIN,
						"vpcVLANMax", VPC_VLAN_MAX,
						"agentRepo", target.Fallback(cfg.AgentRef).RepoName(),
						"agentRepoCA", ZotConfig(get).TLS.CA.Cert,
						"users", users,
						"vpcBackend", cfg.VPCBackend,
						"snatAllowed", cfg.SNATAllowed,
						"vpcSubnet", VPC_SUBNET,
					),
				),
				cnc.KubeHelmChart("fabric-dhcp-server", "default", helm.HelmChartSpec{
					TargetNamespace: "default",
					Chart:           "oci://" + targetInCluster.Fallback(cfg.FabricDHCPServerChartRef).RepoName(),
					Version:         cfg.FabricDHCPServerChartRef.Tag,
					RepoCA:          ZotConfig(get).TLS.CA.Cert,
				}, cnc.FromTemplate(fabricDHCPServerTemplate,
					"ref", target.Fallback(cfg.FabricDHCPServerRef),
				)),
			),
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "kubectl-fabric-install",
		&cnc.FilesORAS{
			Ref: cfg.CtlRef,
			Files: []cnc.File{
				{
					Name:          "hhfctl",
					InstallTarget: "/opt/bin",
					InstallMode:   0o755,
					InstallName:   "kubectl-fabric",
				},
			},
		})

	install(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-wait",
		&cnc.WaitKube{
			Name: "deployment/fabric-controller-manager",
		})

	run(BundleControlInstall, STAGE_INSTALL_3_FABRIC, "fabric-wiring",
		&cnc.FileGenerate{
			File: cnc.File{
				Name:          "wiring.yaml",
				InstallTarget: "/var/lib/rancher/k3s/server/manifests",
				InstallName:   "hh-wiring.yaml",
			},
			Content: cnc.FromValue(wiringData.String()),
		})

	return nil
}
