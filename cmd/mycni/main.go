package main

import (
	"fmt"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/buildversion"
	"net"
	"simple-k8s-cni/pkg/config"
	"simple-k8s-cni/pkg/ipam"
	"simple-k8s-cni/pkg/plugins/bridge"
	"simple-k8s-cni/pkg/store"
)

const (
	pluginName = "mycni"
)

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, buildversion.BuildString(pluginName))
}

func cmdAdd(args *skel.CmdArgs) error {
	conf, err := config.LoadCNIConfig(args.StdinData)
	if err != nil {
		return err
	}

	s, err := store.NewStore(conf.DataDir, conf.Name)
	if err != nil {
		return err
	}
	defer s.Close()

	ipam, err := ipam.NewIPAM(conf, s)
	if err != nil {
		return fmt.Errorf("failed to create ipam: %v", err)
	}

	gateway := ipam.Gateway()

	ip, err := ipam.AllocateIP(args.ContainerID, args.IfName)
	if err != nil {
		return err
	}

	mtu := 1501
	br, err := bridge.CreateBridge(conf.Bridge, mtu, ipam.IPNet(gateway))
	if err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}
	defer netns.Close()

	if err := bridge.SetupVeth(netns, br, mtu, args.IfName, ipam.IPNet(ip), gateway); err != nil {
		return err
	}

	result := &types100.Result{
		CNIVersion: types100.ImplementedSpecVersion,

		IPs: []*types100.IPConfig{
			{
				Address: net.IPNet{IP: ip, Mask: ipam.Mask()},
				Gateway: gateway,
			},
		},
	}
	return types.PrintResult(result, conf.CNIVersion)
}

func cmdCheck(args *skel.CmdArgs) error {
	conf, err := config.LoadCNIConfig(args.StdinData)
	if err != nil {
		return err
	}

	s, err := store.NewStore(conf.DataDir, conf.Name)
	if err != nil {
		return err
	}
	defer s.Close()

	ipam, err := ipam.NewIPAM(conf, s)
	if err != nil {
		return fmt.Errorf("failed to create ipam: %v", err)
	}

	ip, err := ipam.CheckIP(args.ContainerID)
	if err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}
	defer netns.Close()

	return bridge.CheckVeth(netns, args.IfName, ip)
}

func cmdDel(args *skel.CmdArgs) error {
	conf, err := config.LoadCNIConfig(args.StdinData)
	if err != nil {
		return err
	}

	s, err := store.NewStore(conf.DataDir, conf.Name)
	if err != nil {
		return err
	}
	defer s.Close()

	ipam, err := ipam.NewIPAM(conf, s)
	if err != nil {
		return fmt.Errorf("failed to create ipam: %v", err)
	}

	if err := ipam.ReleaseIP(args.ContainerID); err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}
	defer netns.Close()

	return bridge.DelVeth(netns, args.IfName)
}
