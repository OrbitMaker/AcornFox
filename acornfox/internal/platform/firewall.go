package platform

import "strings"

// Windows firewall rule that lets LAN devices reach apps relayed by
// acornfox serve (cmd/acornfox/lanforward.go): inbound TCP to acornfox.exe
// only, on the reserved app ports and the LAN console port, and only on
// private and domain networks (never on public ones, e.g. a cafe Wi-Fi).
// Adding or removing it needs an administrator; checking does not.
const (
	FirewallRuleName  = "AcornFox-LAN"
	FirewallRulePorts = "18810-18859,18802"
)

// FirewallRulePresent reports whether the rule exists. Always true outside
// Windows (nothing to add there).
func FirewallRulePresent() bool {
	if GOOS != "windows" {
		return true
	}
	out, err := RunHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"if (Get-NetFirewallRule -Name "+FirewallRuleName+" -ErrorAction SilentlyContinue) { 'yes' } else { 'no' }")
	return err == nil && strings.Contains(string(out), "yes")
}

// FirewallAddPS is the PowerShell (run elevated) that (re)creates the rule
// for exe.
func FirewallAddPS(exe string) string {
	return "Remove-NetFirewallRule -Name " + FirewallRuleName + " -ErrorAction SilentlyContinue; " +
		"New-NetFirewallRule -Name " + FirewallRuleName + " -DisplayName 'AcornFox LAN apps' -Direction Inbound " +
		"-Program '" + strings.ReplaceAll(exe, "'", "''") + "' -Protocol TCP -LocalPort " + FirewallRulePorts +
		" -Action Allow -Profile Private,Domain | Out-Null"
}

// FirewallRemovePS is the PowerShell (run elevated) that deletes the rule.
func FirewallRemovePS() string {
	return "Remove-NetFirewallRule -Name " + FirewallRuleName + " -ErrorAction SilentlyContinue"
}

// NetworkCategory returns the Windows network category of the interface that
// holds ip: "Public", "Private", "DomainAuthenticated", or "" (unknown, or
// not Windows). LAN devices are blocked on "Public" networks.
func NetworkCategory(ip string) string {
	if GOOS != "windows" || ip == "" {
		return ""
	}
	out, err := RunHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"$a = (Get-NetIPAddress -IPAddress '"+ip+"' -ErrorAction SilentlyContinue).InterfaceAlias; "+
			"if ($a) { (Get-NetConnectionProfile -InterfaceAlias $a -ErrorAction SilentlyContinue).NetworkCategory }")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
