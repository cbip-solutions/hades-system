package aggregator

import (
	"sort"
	"testing"
)

func FuzzFuse_Determinism(f *testing.F) {

	f.Add("a,b,c", "d,e,f", 60, 10)

	f.Add("a,b,c", "b,a,c", 60, 10)

	f.Add("x,y,z", "x,y,z", 60, 10)

	f.Add("a,b", "a,b", 0, 5)

	f.Add("", "", 60, 10)
	f.Add("a", "", 60, 10)

	f.Add("a,b,c", "c,b,a", 1_000_000, 10)

	f.Add("a,b,c,d,e", "", 60, 0)

	f.Add("a,b,c", "b,a,c", -5, 10)

	f.Add(
		"a,b,c,d,e,f,g,h,i,j,k,l,m,n,o,p,q,r,s,t,u,v,w,x,y,z",
		"z,y,x,w,v,u,t,s,r,q,p,o,n,m,l,k,j,i,h,g,f,e,d,c,b,a",
		60, 26,
	)

	f.Add(",,,", ",,,", 60, 10)

	f.Fuzz(func(t *testing.T, listAStr, listBStr string, k, limit int) {

		if len(listAStr) > 1024 || len(listBStr) > 1024 {
			t.Skip()
		}

		if k > 10_000_000 || k < -10_000_000 {
			t.Skip()
		}
		if limit > 1000 || limit < -1000 {
			t.Skip()
		}

		la := splitCSV(listAStr)
		lb := splitCSV(listBStr)
		topKs := []TopK{
			{Source: "fts", Results: idsToResults(la, "fts")},
			{Source: "vec", Results: idsToResults(lb, "vec")},
		}

		got1 := Fuse(topKs, k, limit)

		got2 := Fuse(topKs, k, limit)

		if len(got1) != len(got2) {
			t.Fatalf("non-deterministic length: %d vs %d", len(got1), len(got2))
		}
		for i := range got1 {
			if got1[i].NoteID != got2[i].NoteID {
				t.Fatalf("non-deterministic NoteID at index %d: %q vs %q",
					i, got1[i].NoteID, got2[i].NoteID)
			}
			if got1[i].Score != got2[i].Score {
				t.Fatalf("non-deterministic Score at index %d: %v vs %v",
					i, got1[i].Score, got2[i].Score)
			}
			if got1[i].Source != got2[i].Source {
				t.Fatalf("non-deterministic Source at index %d: %q vs %q",
					i, got1[i].Source, got2[i].Source)
			}
		}

		if !sort.SliceIsSorted(got1, func(i, j int) bool {
			return got1[i].Score > got1[j].Score
		}) {
			t.Fatalf("Fuse output not sorted by descending Score: %v", got1)
		}

		for i, r := range got1 {
			if r.Score <= 0 {
				t.Fatalf("non-positive Score at index %d: %v (NoteID=%q)",
					i, r.Score, r.NoteID)
			}
		}

		effLimit := limit
		if effLimit <= 0 {
			effLimit = defaultQueryLimit
		}
		if len(got1) > effLimit {
			t.Fatalf("output length %d exceeds effective limit %d", len(got1), effLimit)
		}

		distinct := map[string]struct{}{}
		for _, id := range la {
			distinct[id] = struct{}{}
		}
		for _, id := range lb {
			distinct[id] = struct{}{}
		}
		if len(got1) > len(distinct) {
			t.Fatalf("output length %d exceeds distinct NoteID count %d",
				len(got1), len(distinct))
		}
	})
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func idsToResults(ids []string, source string) []QueryResult {
	out := make([]QueryResult, 0, len(ids))
	for _, id := range ids {
		out = append(out, QueryResult{
			NoteID:    id,
			Title:     "title-" + id,
			ProjectID: "proj-fuzz",
			Source:    source,
		})
	}
	return out
}
