import Base

# Facts about natural numbers that the ledger proofs use. Base has none of
# them, and Bend has no proof search, so each is proven here by induction.

law add_zero:
  for +x: Nat
  {Nat.add(x, 0n) == x : Nat}

def add_zero(x):
  match x:
    case 0n:
      {==}
    case 1n+p:
      %add_zero(p) : {1n+Nat.add(p, 0n) == 1n+_ : Nat}
      {==}

law add_succ:
  for +x: Nat
  for +y: Nat
  {Nat.add(x, 1n+y) == 1n+Nat.add(x, y) : Nat}

def add_succ(x, y):
  match x:
    case 0n:
      {==}
    case 1n+p:
      %add_succ(p, y) : {1n+Nat.add(p, 1n+y) == 1n+_ : Nat}
      {==}

law add_assoc:
  for +x: Nat
  for +y: Nat
  for +z: Nat
  {Nat.add(Nat.add(x, y), z) == Nat.add(x, Nat.add(y, z)) : Nat}

def add_assoc(x, y, z):
  match x:
    case 0n:
      {==}
    case 1n+p:
      %add_assoc(p, y, z) : {1n+Nat.add(Nat.add(p, y), z) == 1n+_ : Nat}
      {==}

law add_comm:
  for +x: Nat
  for +y: Nat
  {Nat.add(x, y) == Nat.add(y, x) : Nat}

def add_comm(x, y):
  match x:
    case 0n:
      Equal.sym(Nat, Nat.add(y, 0n), y, add_zero(y))
    case 1n+p:
      %Equal.sym(Nat, Nat.add(y, 1n+p), 1n+Nat.add(y, p), add_succ(y, p)) : {1n+Nat.add(p, y) == _ : Nat}
      %add_comm(p, y) : {1n+Nat.add(p, y) == 1n+_ : Nat}
      {==}

# (x + a) + h == (x + h) + a
law add_swap:
  for +x: Nat
  for +a: Nat
  for +h: Nat
  {Nat.add(Nat.add(x, a), h) == Nat.add(Nat.add(x, h), a) : Nat}

def add_swap(x, a, h):
  %Equal.sym(Nat, Nat.add(Nat.add(x, h), a), Nat.add(x, Nat.add(h, a)), add_assoc(x, h, a)) : {Nat.add(Nat.add(x, a), h) == _ : Nat}
  %add_comm(a, h) : {Nat.add(Nat.add(x, a), h) == Nat.add(x, _) : Nat}
  add_assoc(x, a, h)

law is_eq_same:
  for +x: Nat
  {Nat.is_eq(x, x) == True{} : Bool}

def is_eq_same(x):
  match x:
    case 0n:
      {==}
    case 1n+p:
      is_eq_same(p)

# False is not True: the type below is Unit at False and Empty at True.
def Not.true(b: Bool) -> Type:
  match b:
    case True{}:
      Empty
    case False{}:
      Unit

def false_is_not_true(e: {False{} == True{} : Bool}) -> Empty:
  %e : Not.true(_)
  Unit{}

# When the node's comparison says two numbers are equal, they are.
law is_eq_sound:
  for +x: Nat
  for +y: Nat
  for e: {Nat.is_eq(x, y) == True{} : Bool}
  {x == y : Nat}

def is_eq_sound(x, y, e):
  match x y:
    case 0n 0n:
      {==}
    case 0n 1n+yp:
      Empty.absurd({0n == 1n+yp : Nat}, false_is_not_true(e))
    case 1n+xp 0n:
      Empty.absurd({1n+xp == 0n : Nat}, false_is_not_true(e))
    case 1n+xp 1n+yp:
      %is_eq_sound(xp, yp, e) : {1n+xp == 1n+_ : Nat}
      {==}
