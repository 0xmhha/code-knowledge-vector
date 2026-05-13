package solidity

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestParseExtractsContractMembers(t *testing.T) {
	src := []byte(`// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function totalSupply() external view returns (uint256);
}

contract Token {
    event Transfer(address indexed from, address indexed to, uint256 value);

    mapping(address => uint256) private _balances;

    modifier onlyPositive(uint256 amount) {
        require(amount > 0, "zero amount");
        _;
    }

    constructor() {}

    function totalSupply() public view returns (uint256) {
        return 0;
    }

    function transfer(address to, uint256 amount)
        external
        onlyPositive(amount)
        returns (bool)
    {
        emit Transfer(msg.sender, to, amount);
        return true;
    }
}
`)
	spans, err := New().Parse("token.sol", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]types.SymbolKind{
		"IERC20":               types.KindInterface,
		"IERC20.totalSupply":   types.KindMethod,
		"Token":                types.KindContract,
		"Token.Transfer":       types.KindEvent,
		"Token.onlyPositive":   types.KindModifier,
		"Token.constructor":    types.KindMethod,
		"Token.totalSupply":    types.KindMethod,
		"Token.transfer":       types.KindMethod,
	}
	got := map[string]types.SymbolKind{}
	for _, s := range spans {
		got[s.Name] = s.Kind
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("missing/wrong kind for %s: got %s, want %s", name, got[name], kind)
		}
	}
}

func TestParseLibraryAsContract(t *testing.T) {
	src := []byte(`// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}
`)
	spans, err := New().Parse("sm.sol", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var libFound, fnFound bool
	for _, s := range spans {
		if s.Name == "SafeMath" && s.Kind == types.KindContract {
			libFound = true
		}
		if s.Name == "SafeMath.add" && s.Kind == types.KindMethod {
			fnFound = true
		}
	}
	if !libFound {
		t.Errorf("library SafeMath not extracted as Contract: %+v", spans)
	}
	if !fnFound {
		t.Errorf("library method add not extracted: %+v", spans)
	}
}

func TestParseHandlesStructAndEnum(t *testing.T) {
	src := []byte(`// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Box {
    enum Color { Red, Green, Blue }

    struct Item {
        uint256 id;
        string name;
    }
}
`)
	spans, _ := New().Parse("box.sol", src)
	var enumOK, structOK bool
	for _, s := range spans {
		if s.Name == "Box.Color" && s.Kind == types.KindType {
			enumOK = true
		}
		if s.Name == "Box.Item" && s.Kind == types.KindStruct {
			structOK = true
		}
	}
	if !enumOK {
		t.Errorf("enum Color not extracted as KindType: %+v", spans)
	}
	if !structOK {
		t.Errorf("struct Item not extracted as KindStruct: %+v", spans)
	}
}
