package config

import (
	"encoding/json"
	"fmt"
	"net"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

type outboundMap map[string]interface{}

func patchOutboundMux(base option.Outbound, configOpt ConfigOptions, obj outboundMap) outboundMap {
	if configOpt.EnableMux {
		multiplex := option.OutboundMultiplexOptions{
			Enabled:    true,
			Padding:    configOpt.MuxPadding,
			MaxStreams: configOpt.MaxStreams,
			Protocol:   configOpt.MuxProtocol,
		}
		obj["multiplex"] = multiplex
		// } else {
		// 	delete(obj, "multiplex")
	}
	return obj
}

func patchOutboundTLSTricks(base option.Outbound, configOpt ConfigOptions, obj outboundMap) outboundMap {

	if base.Type == C.TypeSelector || base.Type == C.TypeURLTest || base.Type == C.TypeBlock || base.Type == C.TypeDNS {
		return obj
	}
	if isOutboundReality(base) {
		return obj
	}

	var tls *option.OutboundTLSOptions
	var transport *option.V2RayTransportOptions
	if base.VLESSOptions.OutboundTLSOptionsContainer.TLS != nil {
		tls = base.VLESSOptions.OutboundTLSOptionsContainer.TLS
		transport = base.VLESSOptions.Transport
	} else if base.TrojanOptions.OutboundTLSOptionsContainer.TLS != nil {
		tls = base.TrojanOptions.OutboundTLSOptionsContainer.TLS
		transport = base.TrojanOptions.Transport
	} else if base.VMessOptions.OutboundTLSOptionsContainer.TLS != nil {
		tls = base.VMessOptions.OutboundTLSOptionsContainer.TLS
		transport = base.VMessOptions.Transport
	}

	if base.Type == C.TypeDirect {
		return patchOutboundFragment(base, configOpt, obj)
	}

	if tls == nil || !tls.Enabled || transport == nil {
		return obj
	}

	if transport.Type != C.V2RayTransportTypeWebsocket && transport.Type != C.V2RayTransportTypeGRPC && transport.Type != C.V2RayTransportTypeHTTPUpgrade {
		return obj
	}

	if outtls, ok := obj["tls"].(map[string]interface{}); ok {
		obj = patchOutboundFragment(base, configOpt, obj)
		tlsTricks := tls.TLSTricks
		if tlsTricks == nil {
			tlsTricks = &option.TLSTricksOptions{}
		}
		tlsTricks.MixedCaseSNI = tlsTricks.MixedCaseSNI || configOpt.TLSTricks.EnableMixedSNICase

		if configOpt.TLSTricks.EnablePadding {
			tlsTricks.PaddingMode = "random"
			tlsTricks.PaddingSize = configOpt.TLSTricks.PaddingSize
			// fmt.Printf("--------------------%+v----%+v", tlsTricks.PaddingSize, configOpt)
			outtls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": "custom",
			}
		}

		outtls["tls_tricks"] = tlsTricks
		// if tlsTricks.MixedCaseSNI || tlsTricks.PaddingMode != "" {
		// 	// } else {
		// 	// 	tls["tls_tricks"] = nil
		// }
		// fmt.Printf("-------%+v------------- ", tlsTricks)
	}
	return obj
}

func patchOutboundFragment(base option.Outbound, configOpt ConfigOptions, obj outboundMap) outboundMap {
	if configOpt.EnableFragment {

		obj["tls_fragment"] = option.TLSFragmentOptions{
			Enabled: configOpt.TLSTricks.EnableFragment,
			Size:    configOpt.TLSTricks.FragmentSize,
			Sleep:   configOpt.TLSTricks.FragmentSleep,
		}

	}

	return obj
}

func isOutboundReality(base option.Outbound) bool {
	// this function checks reality status ONLY FOR VLESS.
	// Some other protocols can also use reality, but it's discouraged as stated in the reality document
	if base.Type != C.TypeVLESS {
		return false
	}
	if base.VLESSOptions.OutboundTLSOptionsContainer.TLS == nil {
		return false
	}
	if base.VLESSOptions.OutboundTLSOptionsContainer.TLS.Reality == nil {
		return false
	}
	return base.VLESSOptions.OutboundTLSOptionsContainer.TLS.Reality.Enabled

}

func patchOutbound(base option.Outbound, configOpt ConfigOptions) (*option.Outbound, string, error) {

	formatErr := func(err error) error {
		return fmt.Errorf("error patching outbound[%s][%s]: %w", base.Tag, base.Type, err)
	}
	err := patchWarp(&base)
	if err != nil {
		return nil, "", formatErr(err)
	}
	var outbound option.Outbound

	jsonData, err := base.MarshalJSON()
	if err != nil {
		return nil, "", formatErr(err)
	}

	var obj outboundMap
	err = json.Unmarshal(jsonData, &obj)
	if err != nil {
		return nil, "", formatErr(err)
	}
	var serverDomain string
	if server, ok := obj["server"].(string); ok {
		if server != "" && net.ParseIP(server) == nil {
			serverDomain = fmt.Sprintf("full:%s", server)
		}
	}

	obj = patchOutboundTLSTricks(base, configOpt, obj)

	switch base.Type {
	case C.TypeVMess, C.TypeVLESS, C.TypeTrojan, C.TypeShadowsocks:
		obj = patchOutboundMux(base, configOpt, obj)
	}

	modifiedJson, err := json.Marshal(obj)
	if err != nil {
		return nil, "", formatErr(err)
	}

	err = outbound.UnmarshalJSON(modifiedJson)
	if err != nil {
		return nil, "", formatErr(err)
	}

	return &outbound, serverDomain, nil
}

func patchWarp(base *option.Outbound) error {
	if base.Type == C.TypeWireGuard {
		host := base.WireGuardOptions.Server
		if host == "default" || host == "random" || host == "auto" || isBlockedDomain(host) {
			base.WireGuardOptions.Server = getRandomIP()
		}
		if base.WireGuardOptions.ServerPort == 0 {
			base.WireGuardOptions.ServerPort = generateRandomPort()
		}
	}
	if base.Type == C.TypeCustom {
		if warp, ok := base.CustomOptions["warp"].(map[string]interface{}); ok {
			key, _ := warp["key"].(string)
			host, _ := warp["host"].(string)
			port, _ := warp["port"].(float64)
			detour, _ := warp["detour"].(string)
			fakePackets, _ := warp["fake_packets"].(string)
			fakePacketsSize, _ := warp["fake_packets_size"].(string)
			fakePacketsDelay, _ := warp["fake_packets_delay"].(string)
			warpConfig, err := generateWarp(key, host, uint16(port), fakePackets, fakePacketsSize, fakePacketsDelay)
			if err != nil {
				fmt.Printf("Error generating warp config: %v", err)
				return err
			}

			base.Type = C.TypeWireGuard
			warpConfig.WireGuardOptions.Detour = detour
			if detour != "" {
				if warpConfig.WireGuardOptions.MTU > 1000 {
					warpConfig.WireGuardOptions.MTU -= 160
				}
				warpConfig.WireGuardOptions.FakePackets = ""
			}
			base.WireGuardOptions = warpConfig.WireGuardOptions

		}

	}

	return nil
}

// func (o outboundMap) transportType() string {
// 	if transport, ok := o["transport"].(map[string]interface{}); ok {
// 		if transportType, ok := transport["type"].(string); ok {
// 			return transportType
// 		}
// 	}
// 	return ""
// }
