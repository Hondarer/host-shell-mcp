package tools

import "testing"

func TestFinalToolNames(t *testing.T) {
	catalog := Catalog()
	for _, name := range []string{"help_get", "sys_get_info", "proc_list", "disk_list", "exec_sudo", "search_find", "net_request_http", "net_exchange", "net_list_listeners", "net_list_interfaces", "reg_read", "reg_write", "reg_delete"} {
		if _, ok := catalog[name]; !ok {
			t.Errorf("missing tool %s", name)
		}
	}
	for _, name := range []string{"help", "sys_info", "sys_processes", "sys_disks", "sudo_run", "search_files", "net_http", "net_tcp", "net_listen", "net_interfaces"} {
		if _, ok := catalog[name]; ok {
			t.Errorf("unexpected tool %s", name)
		}
	}
}
