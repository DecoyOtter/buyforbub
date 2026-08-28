# Buy for Bub

A shared checklist of things to buy before a baby arrives, and the candidate
products being weighed up for each one.

## Language

### The checklist

**Item**:
One thing that needs acquiring, named generically — "Cot", "Pram", "Car seat".
It states a need, never a particular product.
_Avoid_: Product, entry, row, task, line

**Category**:
The fixed bucket an Item sits in. One of Nursery, Feeding, Clothing, Travel,
Health, Other, always displayed in that order.
_Avoid_: Group, section, tag

**Status**:
Whether an Item is still `needed` or has been `bought`. Nothing in between.
_Avoid_: State, done, complete

**Qty**:
How many of an Item are wanted. Informational — buying three of four does not
make the Item partly bought.
_Avoid_: Count, amount

**Progress**:
Bought Items over total Items, across the whole list rather than per Category.

**Seed**:
The default set of Items a fresh database starts with. Applied once, only to an
empty list.
_Avoid_: Template, defaults, preset

### Deciding what to buy

**Option**:
A specific product being considered for an Item — "Seena Cot, $1500", pointing
at a shop. An Item has many; they compete with each other.
_Avoid_: Link, candidate, product, choice, suggestion

**Label**:
The name of an Option. Optional — the shop's hostname stands in when it is
blank.
_Avoid_: Title, name, description

**Chosen**:
The one Option that was actually bought. At most one per Item, and choosing it
is what marks the Item bought.
_Avoid_: Selected, picked, winner, decided

**Comment**:
A remark left against one Option while it is being weighed up — "has good
rounded edges for leaning", "too expensive". An Option has many, in the order
they were written. Nobody owns a Comment; the list is shared, so is the opinion.
_Avoid_: Note, review, feedback, pro, con

**Choosing**:
Committing to an Option. It marks the Item bought and records which Option won,
in one action. Marking the Item back to needed un-chooses it, so the record
never claims something was bought when it was not.

## Relationships

- An Item may have no Options at all. Ticking it bought directly is normal — not
  everything needs comparing.
- A **chosen** Option implies its Item is **bought**. The reverse does not hold:
  a bought Item may have no chosen Option.
- Options never carry a Status. Only Items are needed or bought.
- Comments hang off an Option, never off an Item. Deleting the Option — or its
  Item — takes them with it.
- Choosing an Option, or backing that out, says nothing about its Comments. A
  losing Option keeps the reasons it lost.
