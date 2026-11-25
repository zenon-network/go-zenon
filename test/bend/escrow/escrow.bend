import Base
import ../ledger/ledger.bend as L

# A model of a contract that holds value in entries and pays each entry
# out: the HTLC and PTLC contracts of go-zenon, and the locks of the Stake
# and Plasma contracts (vm/embedded/implementation/htlc.go, ptlc.go,
# stake.go, plasma.go).
# One token; accounts and entries are numbered; time is in seconds.
#
#   node                                         model
#   ----                                         -----
#   CreateHtlcMethod.ReceiveBlock, HtlcInfo.Save Create
#   UnlockHtlcMethod.ReceiveBlock                Unlock
#   ReclaimHtlcMethod.ReceiveBlock               Reclaim
#   unlockPtlc (Unlock, ProxyUnlock)             Claim
#   ReclaimPtlcMethod.ReceiveBlock               Reclaim
#   HtlcInfo.Delete, PtlcInfo.Delete             the entry leaves the list
#   StakeMethod, FuseMethod                      Create, with the maker as payee
#   CancelFuseMethod (FusionInfo.Delete)         Reclaim
#   CancelStakeMethod (before DynamicPlasmaSpork) Cancel
#   CancelStakeMethod (from DynamicPlasmaSpork)  Reclaim (active entries)
#   VM.rollbackEmbedded (the refund)             Failed
#   the timestamp of the confirming momentum     now
#
# The balances are the ones of the ledger rule (../ledger). A payout is
# credited straight to the account; that it cannot be received twice on
# the way is what the ledger rule is about.

# One entry: who made it, who is paid when it is unlocked, how much it
# holds, and when it expires.
type Entry is Data:
  Entry{id: Nat, maker: Nat, payee: Nat, amount: Nat, expiry: Nat}

# A call to the contract, or a momentum (ten seconds).
#   Unlock is HTLC's: key is whether the caller shows the right preimage,
#   and the entry's payee is paid.
#   Claim is PTLC's unlock: key is whether the caller shows a signature of
#   the entry's point over this entry and the account to, and to is paid.
#   Reclaim pays the maker from the expiry on and removes the entry: HTLC,
#   PTLC, the cancel of a fusion, and (for active entries) a stake cancel
#   from DynamicPlasmaSpork.
#   Cancel is the pre-DynamicPlasmaSpork cancel of a stake: it pays the
#   maker from the expiry on and leaves the entry in place, holding nothing.
#   Failed is a call to any contract that carries an amount and that the
#   contract refuses: the amount leaves the caller with the call and comes
#   back with the refund.
type Call is Data:
  Create{from: Nat, payee: Nat, amount: Nat, expiry: Nat}
  Unlock{id: Nat, key: Bool}
  Claim{id: Nat, key: Bool, to: Nat}
  Reclaim{from: Nat, id: Nat}
  Cancel{from: Nat, id: Nat}
  Failed{from: Nat, amount: Nat}
  Tick{}

# The accounts' balances, the entries the contract holds, the time, and
# the number the next entry gets.
type Vault is Data:
  Vault{bals: +List<Nat>, entries: +List<Entry>, now: Nat, next: Nat}

# The answer to a call: refused, or the vault afterwards.
type Out is Data:
  Refused{}
  Accepted{vault: Vault}

# The answer to a payout: nothing to pay, or whom, how much, and the
# entries that are left.
type Found is Data:
  Gone{}
  Paid{to: Nat, amount: Nat, entries: +List<Entry>}

# Entries
# -------

# The value the contract holds.
def Entries.held(+es: +List<Entry>) -> Nat:
  match es:
    case Nil{}:
      0n
    case Con{Entry{id, maker, payee, amount, expiry}, t}:
      Nat.add(Entries.held(t), amount)

def Entries.count(+es: +List<Entry>) -> Nat:
  match es:
    case Nil{}:
      0n
    case Con{e, t}:
      1n+Entries.count(t)

def Entries.pick(+hit: Bool, +here: Found, +rest: Found) -> Found:
  match hit:
    case True{}:
      here
    case False{}:
      rest

def Entries.skip(+e: Entry, +f: Found) -> Found:
  match f:
    case Gone{}:
      Gone{}
    case Paid{to, amount, entries}:
      Paid{to, amount, Con{e, entries}}

# The entry is the one asked for: pay it and drop it when the call is
# allowed, else pay nothing.
def Entries.here(+ok: Bool, +to: Nat, +amount: Nat, +t: +List<Entry>) -> Found:
  match ok:
    case False{}:
      Gone{}
    case True{}:
      Paid{to, amount, t}

# Unlock is allowed when the preimage is right and the entry has not
# expired.
def Entries.may_unlock(+key: Bool, +now: Nat, +expiry: Nat) -> Bool:
  Bool.and(key, Nat.is_lt(now, expiry))

# A claim is allowed when the signature is valid and the entry has not
# expired.
def Entries.may_claim(+key: Bool, +now: Nat, +expiry: Nat) -> Bool:
  Bool.and(key, Nat.is_lt(now, expiry))

# Reclaim is allowed when the caller made the entry and it has expired.
def Entries.may_reclaim(+from: Nat, +maker: Nat, +now: Nat, +expiry: Nat) -> Bool:
  Bool.and(Nat.is_eq(from, maker), Nat.is_ge(now, expiry))

# Unlock: the payee is paid, whoever calls.
def Entries.unlock(+es: +List<Entry>, +want: Nat, +key: Bool, +now: Nat) -> Found:
  match es:
    case Nil{}:
      Gone{}
    case Con{Entry{id, maker, payee, amount, expiry}, t}:
      Entries.pick(Nat.is_eq(id, want), Entries.here(Entries.may_unlock(key, now, expiry), payee, amount, t), Entries.skip(Entry{id, maker, payee, amount, expiry}, Entries.unlock(t, want, key, now)))

# Claim: the account named in the signed message is paid.
def Entries.claim(+es: +List<Entry>, +want: Nat, +key: Bool, +to: Nat, +now: Nat) -> Found:
  match es:
    case Nil{}:
      Gone{}
    case Con{Entry{id, maker, payee, amount, expiry}, t}:
      Entries.pick(Nat.is_eq(id, want), Entries.here(Entries.may_claim(key, now, expiry), to, amount, t), Entries.skip(Entry{id, maker, payee, amount, expiry}, Entries.claim(t, want, key, to, now)))

# Reclaim: the maker is paid.
def Entries.reclaim(+es: +List<Entry>, +want: Nat, +from: Nat, +now: Nat) -> Found:
  match es:
    case Nil{}:
      Gone{}
    case Con{Entry{id, maker, payee, amount, expiry}, t}:
      Entries.pick(Nat.is_eq(id, want), Entries.here(Entries.may_reclaim(from, maker, now, expiry), maker, amount, t), Entries.skip(Entry{id, maker, payee, amount, expiry}, Entries.reclaim(t, want, from, now)))

# Cancel (pre-DynamicPlasmaSpork): the maker is paid what the entry holds,
# and the entry stays, holding nothing. Before DynamicPlasmaSpork a cancel
# of an entry that was cancelled before is accepted too, and pays zero.
def Entries.cancel(+es: +List<Entry>, +want: Nat, +from: Nat, +now: Nat) -> Found:
  match es:
    case Nil{}:
      Gone{}
    case Con{Entry{id, maker, payee, amount, expiry}, t}:
      Entries.pick(Nat.is_eq(id, want), Entries.here(Entries.may_reclaim(from, maker, now, expiry), maker, amount, Con{Entry{id, maker, payee, 0n, expiry}, t}), Entries.skip(Entry{id, maker, payee, amount, expiry}, Entries.cancel(t, want, from, now)))

# Calls
# -----

def Step.create.pay(+m: Maybe<&2, +List<Nat>>, +entries: +List<Entry>, +now: Nat, +next: Nat, +from: Nat, +payee: Nat, +amount: Nat, +expiry: Nat) -> Out:
  match m:
    case None{}:
      Refused{}
    case Some{bals}:
      Accepted{Vault{bals, Con{Entry{next, from, payee, amount, expiry}, entries}, now, 1n+next}}

# Create: the entry must hold something and must not be expired already,
# and the maker's balance must cover the amount, which moves into the entry.
def Step.create(+live: Bool, +bals: +List<Nat>, +entries: +List<Entry>, +now: Nat, +next: Nat, +from: Nat, +payee: Nat, +amount: Nat, +expiry: Nat) -> Out:
  match live:
    case False{}:
      Refused{}
    case True{}:
      Step.create.pay(L.Bal.debit(from, bals, amount), entries, now, next, from, payee, amount, expiry)

def Step.pay(+f: Found, +bals: +List<Nat>, +now: Nat, +next: Nat) -> Out:
  match f:
    case Gone{}:
      Refused{}
    case Paid{to, amount, entries}:
      Accepted{Vault{L.Bal.credit(to, bals, amount), entries, now, next}}

# A call the contract refuses: the amount is taken with the call, when the
# balance covers it, and given back.
def Step.fail(+m: Maybe<&2, +List<Nat>>, +entries: +List<Entry>, +now: Nat, +next: Nat, +from: Nat, +amount: Nat) -> Out:
  match m:
    case None{}:
      Refused{}
    case Some{bals}:
      Accepted{Vault{L.Bal.credit(from, bals, amount), entries, now, next}}

def step(+v: Vault, +c: Call) -> Out:
  match v c:
    case Vault{bals, entries, now, next} Create{from, payee, amount, expiry}:
      Step.create(Bool.and(Nat.is_gt(amount, 0n), Nat.is_lt(now, expiry)), bals, entries, now, next, from, payee, amount, expiry)
    case Vault{bals, entries, now, next} Unlock{id, key}:
      Step.pay(Entries.unlock(entries, id, key, now), bals, now, next)
    case Vault{bals, entries, now, next} Claim{id, key, to}:
      Step.pay(Entries.claim(entries, id, key, to, now), bals, now, next)
    case Vault{bals, entries, now, next} Reclaim{from, id}:
      Step.pay(Entries.reclaim(entries, id, from, now), bals, now, next)
    case Vault{bals, entries, now, next} Cancel{from, id}:
      Step.pay(Entries.cancel(entries, id, from, now), bals, now, next)
    case Vault{bals, entries, now, next} Failed{from, amount}:
      Step.fail(L.Bal.debit(from, bals, amount), entries, now, next, from, amount)
    case Vault{bals, entries, now, next} Tick{}:
      Accepted{Vault{bals, entries, Nat.add(now, 10n), next}}

def keep(+v: Vault, +o: Out) -> Vault:
  match o:
    case Refused{}:
      v
    case Accepted{vault}:
      vault

def apply(+v: Vault, +c: Call) -> Vault:
  keep(v, step(v, c))

def run(+cs: +List<Call>, +v: Vault) -> Vault:
  match cs:
    case Nil{}:
      v
    case Con{c, t}:
      run(t, apply(v, c))

# All the value there is: what the accounts hold plus what the contract
# holds in entries.
def worth(+v: Vault) -> Nat:
  match v:
    case Vault{bals, entries, now, next}:
      Nat.add(L.Bal.sum(bals), Entries.held(entries))
