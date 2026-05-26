package solidity

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/parse/fuzzcheck"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(`// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;
contract Token {
    function transfer(address to, uint256 amount) public returns (bool) {
        return true;
    }
    event Transfer(address indexed from, address indexed to, uint256 value);
}
`))
	f.Add([]byte(`pragma solidity ^0.8.0;
interface IERC20 { function balanceOf(address) external view returns (uint256); }
`))
	f.Add([]byte(``))
	f.Add([]byte(`not solidity at all ###`))

	p := New()
	f.Fuzz(func(t *testing.T, src []byte) {
		spans, err := p.Parse("fuzz.sol", src)
		if err != nil {
			return
		}
		fuzzcheck.CheckSpans(t, spans)
	})
}
