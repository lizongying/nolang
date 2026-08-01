package llvm

import (
	"fmt"
	"os"
	"sync"
)

var dfMu sync.Mutex
var dfCounts = map[string]int{}
var dfBlocks, dfReach, dfSites int

func dfStat(t triState, nblocks, nreach int) {
	dfMu.Lock()
	defer dfMu.Unlock()
	dfCounts[t.String()]++
	dfBlocks += nblocks
	dfReach += nreach
	dfSites++
}

func dfStatNoBlock() {
	dfMu.Lock()
	defer dfMu.Unlock()
	dfCounts["noblock"]++
}

func DFStatDump() {
	if os.Getenv("NOLANG_DF_STATS") == "" {
		return
	}
	dfMu.Lock()
	defer dfMu.Unlock()
	fmt.Fprintf(os.Stderr, "DFSTAT must=%d may=%d mustNot=%d noblock=%d sites=%d avgBlocks=%.1f avgReach=%.1f\n",
		dfCounts["must"], dfCounts["may"], dfCounts["mustNot"], dfCounts["noblock"], dfSites,
		float64(dfBlocks)/float64(max(dfSites, 1)), float64(dfReach)/float64(max(dfSites, 1)))
}
