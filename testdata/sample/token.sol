// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// Solidity fixture for cross-language indexing tests. Mirrors the
/// Go server + TS handler in spirit: a single named "contract" with
/// state, modifier, event, and a couple of methods.

interface IToken {
    function balanceOf(address account) external view returns (uint256);
}

contract Token is IToken {
    event Transfer(address indexed from, address indexed to, uint256 value);

    mapping(address => uint256) private _balances;
    uint256 private _totalSupply;

    /// Reverts when `amount` is zero so accidental empty transfers fail
    /// loudly rather than emitting a no-op Transfer event.
    modifier onlyPositive(uint256 amount) {
        require(amount > 0, "amount must be positive");
        _;
    }

    constructor(uint256 initial) {
        _balances[msg.sender] = initial;
        _totalSupply = initial;
    }

    function balanceOf(address account) public view returns (uint256) {
        return _balances[account];
    }

    /// transfer moves `amount` from msg.sender to `to`. Emits a
    /// Transfer event on success.
    function transfer(address to, uint256 amount)
        external
        onlyPositive(amount)
        returns (bool)
    {
        require(_balances[msg.sender] >= amount, "insufficient balance");
        _balances[msg.sender] -= amount;
        _balances[to] += amount;
        emit Transfer(msg.sender, to, amount);
        return true;
    }
}
