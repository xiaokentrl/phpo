// cache 组规则（1）：offline-prune
package preflight

import "phpo/pkg/errs"

func (r *run) offlinePrune() {
	c := r.c
	for _, x := range r.w.Offline {
		if x.Kind == c.Svc && x.Version == c.Ver {
			return
		}
	}
	r.errf("%s: %s/%s", errs.OfflineMissing, c.Svc, c.Ver)
}
