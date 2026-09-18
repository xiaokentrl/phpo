// hosts 行级纯函数：解析/新增/删除/查询 `127.0.0.1 {domain}` 条目（不提权、不触盘，供单测）
package hosts

import "strings"

// splitLine 返回一行的 IP 与主机名列表；注释与空行 ok=false
func splitLine(line string) (ip string, names []string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", nil, false
	}
	fields := strings.Fields(t)
	if len(fields) < 2 {
		return "", nil, false
	}
	return fields[0], fields[1:], true
}

// Has 是否存在把 domain 指向 ip 的非注释条目
func Has(content, ip, domain string) bool {
	for _, line := range strings.Split(content, "\n") {
		if lip, names, ok := splitLine(line); ok && lip == ip {
			for _, n := range names {
				if strings.EqualFold(n, domain) {
					return true
				}
			}
		}
	}
	return false
}

// Add 追加 `ip domain`；已存在则原样返回、changed=false（幂等）
func Add(content, ip, domain string) (string, bool) {
	if Has(content, ip, domain) {
		return content, false
	}
	nl := "\n" + ip + "\t" + domain
	if content == "" || strings.HasSuffix(content, "\n") {
		return content + ip + "\t" + domain + "\n", true
	}
	return content + nl + "\n", true
}

// Remove 从指向 ip 的行中删除 domain；无匹配则 changed=false。保留注释与其它 IP 的映射。
func Remove(content, ip, domain string) (string, bool) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		lip, names, ok := splitLine(line)
		if !ok || lip != ip {
			out = append(out, line)
			continue
		}
		kept := make([]string, 0, len(names))
		for _, n := range names {
			if strings.EqualFold(n, domain) {
				changed = true
				continue
			}
			kept = append(kept, n)
		}
		if len(kept) > 0 {
			out = append(out, ip+"\t"+strings.Join(kept, " "))
		}
	}
	return strings.Join(out, "\n"), changed
}
