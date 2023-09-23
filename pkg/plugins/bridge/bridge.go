package bridge

import (
	"errors"
	"fmt"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"math/rand"
	"net"
	"os"
	"syscall"
)

/*
	1.# 创建 veth0 和 veth1 一对veth
	ip link add veth0 type veth peer name veth1

	2.# 把 veth0 插入到 network namespace中
	ip link set veth0 netns net1

	3.# 给 network namespace 中的 veth0 配置上ip
	ip netns exec net1 ip addr add 192.168.0.2/24 dev veth0

	4.# 启动 network namespace 中的 veth0
	ip netns exec net1 ip link set veth0 up

5.创建bridge，开启bridge

6.把veth1插入到bridge

7.给bridge配置路由ip

添加默认路由

*/

func CreateBridge(bridge string, mtu int, gateway *net.IPNet) (netlink.Link, error) {

	if l, _ := netlink.LinkByName(bridge); l != nil {
		return l, nil
	}

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			MTU:    mtu,    // 传输最大数据块的大小，网络层大小为 1500bytes
			Name:   bridge, //
			TxQLen: -1,     //
		},
	}

	// 添加bridge设备
	if err := netlink.LinkAdd(br); err != nil && err != syscall.EEXIST {
		return nil, err
	}

	// 防止万一，先查一遍
	dev, err := netlink.LinkByName(bridge)
	if err != nil {
		return nil, err
	}

	// 添加ip
	if err := netlink.AddrAdd(dev, &netlink.Addr{IPNet: gateway}); err != nil {
		return nil, err
	}

	// 启动bridge
	if err := netlink.LinkSetUp(dev); err != nil {
		return nil, err
	}

	return dev, nil
}

func CreateVethPair(ifName string, mtu int, hostName ...string) (*netlink.Veth, *netlink.Veth, error) {
	var vethPairName string

	if len(hostName) > 0 && hostName[0] != "" {
		vethPairName = hostName[0]
	} else {
		for { // 因为是随机生成的名字，为了防止名字重复，所以这里使用了循环

			vethPairName, err := RandomVethName()
			if err != nil {
				logrus.Error(err)
				return nil, nil, err
			}
			_, err = netlink.LinkByName(vethPairName)
			if err != nil && !os.IsExist(err) {
				// 上面生成随机名字可能会重名, 所以这里先尝试按照这个名字获取一下
				// 如果没有这个名字的设备, 那就可以 break 了
				break
			}
		}
	}

	if vethPairName == "" {
		return nil, nil, errors.New("create veth pair's name error")
	}

	// 配置veth对参数，没有配置ns
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			MTU:  mtu,
			Name: ifName,
		},
		PeerName: vethPairName,
	}

	// 创建veth对
	if err := netlink.LinkAdd(veth); err != nil {
		return nil, nil, errors.New("create veth device failed: " + err.Error())
	}

	// 尝试重新获取 veth 设备看是否能成功
	veth1, err := netlink.LinkByName(ifName) // veth1 一会儿要在 pod(net ns) 里
	if err != nil {
		// 如果获取失败就尝试删掉
		netlink.LinkDel(veth1)
		return nil, nil, errors.New("创建完 veth 但是获取失败, err: " + err.Error())
	}

	// 尝试重新获取 veth 设备看是否能成功
	veth2, err := netlink.LinkByName(vethPairName) // veth2 在主机上
	if err != nil {
		// 如果获取失败就尝试删掉
		netlink.LinkDel(veth2)
		return nil, nil, errors.New("创建完 veth 但是获取失败, err: " + err.Error())
	}

	return veth1.(*netlink.Veth), veth2.(*netlink.Veth), nil
}

func SetupVeth(netns ns.NetNS, br netlink.Link, mtu int, ifName, vethPairName string, podIP *net.IPNet, gateway net.IP) error {
	hostIface := &types100.Interface{}
	err := netns.Do(func(hostNS ns.NetNS) error {
		//在容器网络空间创建虚拟网卡,一端在容器内，一端在主机上
		hostVeth, containerVeth, err := ip.SetupVeth(ifName, mtu, "", hostNS)
		if err != nil {
			return err
		}

		hostIface.Name = hostVeth.Name

		// set ip for container veth
		conLink, err := netlink.LinkByName(containerVeth.Name)
		if err != nil {
			return err
		}

		// 绑定Pod IP
		if err := netlink.AddrAdd(conLink, &netlink.Addr{
			IPNet: podIP,
		}); err != nil {
			return err
		}

		// 启动网卡
		if err := netlink.LinkSetUp(conLink); err != nil {
			return err
		}

		// 添加默认路径，网关即网桥的地址
		if err := ip.AddDefaultRoute(gateway, conLink); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	// need to lookup hostVeth again as its index has changed during ns move
	hostVeth, err := netlink.LinkByName(hostIface.Name)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", hostIface.Name, err)
	}

	// 将虚拟网卡另一端绑定到网桥上
	if err := netlink.LinkSetMaster(hostVeth, br); err != nil {
		return fmt.Errorf("failed to connect %q to bridge %v: %v", hostVeth.Attrs().Name, br.Attrs().Name, err)
	}
	return nil
}

func RandomVethName() (string, error) {
	entropy := make([]byte, 4)
	_, err := rand.Read(entropy)
	if err != nil {
		return "", fmt.Errorf("failed to generate random veth name: %v", err)
	}

	return fmt.Sprintf("veth%x", entropy), nil
}

func CheckVeth(netns ns.NetNS, ifName string, ip net.IP) error {
	return netns.Do(func(ns.NetNS) error {
		l, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}

		ips, err := netlink.AddrList(l, netlink.FAMILY_V4)
		if err != nil {
			return err
		}

		for _, addr := range ips {
			if addr.IP.Equal(ip) {
				return nil
			}
		}

		return fmt.Errorf("failed to find ip %s for %s", ip, ifName)
	})
}
