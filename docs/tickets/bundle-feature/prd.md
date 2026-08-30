# Buy for Bub Bundle Feature
**Description:** Add first-class Bundles that create linked Bundle Options across multiple Items, split the purchase price evenly, and behave as one atomic purchase. Use the selected Option C rail UI on desktop, stacked above the checklist on mobile.
**Plan:** [Bundle feature plan](../../plans/bundle-feature.plan.md)

---

## User Stories
### US-001: Persist Bundles and their relationships

**User Story:**
> As a user, I want Bundles stored as first-class records so that their linked Options stay coherent.

**Details:**
* **Priority:** 1
* **Passes:** `true`
* **Notes:** *None*

**Acceptance Criteria:**
* [x] `CONTEXT.md` defines Bundle, Bundle member, Bundle Option, Component label, and Bundle Comment using existing Item and Option vocabulary
* [x] `bundles` stores required name, absolute HTTP/HTTPS URL, price cents, optional regular-price cents, and creation time
* [x] `bundle_members` links one Bundle to distinct Items and generated Options, with a unique stable position and optional Component label
* [x] `bundle_comments` stores insertion-ordered Comments belonging to one Bundle
* [x] Foreign keys and indexes cover Bundle, Item, Option, and Bundle Comment lookup and cascade paths
* [x] Opening an existing database adds the new tables without changing existing Item, Option, or Comment data
* [x] Tests pass
* [x] Typecheck passes

### US-002: Create and read Bundles in the Store

**User Story:**
> As a user, I want to create a Bundle from Items so that each Item receives its share as a Bundle Option.

**Details:**
* **Priority:** 2
* **Passes:** `true`
* **Notes:** *None*

**Acceptance Criteria:**
* [x] Bundle input requires a trimmed name, an absolute HTTP/HTTPS URL, a price greater than zero with at most two decimal places, and at least two distinct existing Items
* [x] Optional regular price accepts at most two decimal places and must be greater than or equal to the Bundle price
* [x] Creating a Bundle transactionally inserts one Bundle, ordered members, and one unchosen Bundle Option per member without changing Item Status
* [x] Bundle Options use the Bundle URL and receive equal integer-cent shares whose sum exactly equals the Bundle price
* [x] Remainder cents go to the earliest member positions; Item Qty does not affect allocation
* [x] A blank Component label remains blank so the Item name can be used as its display fallback
* [x] Bought and needed Items, cross-Category membership, overlapping Bundles, and duplicate Bundle names or URLs are accepted
* [x] Only selected tracked Items become members; untracked package contents and standalone component prices require no records
* [x] Get and list operations return Bundles in creation order and members in stable position order
* [x] Invalid input or a missing Item inserts nothing
* [x] Tests pass
* [x] Typecheck passes

### US-003: Persist Bundle Comments

**User Story:**
> As a user, I want shared Bundle Comments so that package-wide considerations are visible from every member Item.

**Details:**
* **Priority:** 3
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Store operations add, get, list, and delete Bundle Comments
* [ ] Bundle Comment bodies are trimmed and blank bodies are rejected
* [ ] Bundle Comments are returned oldest first and remain separate from Option Comments
* [ ] Missing Bundles and Bundle Comments return `ErrNotFound`
* [ ] Tests pass
* [ ] Typecheck passes

### US-004: Edit Bundles and recalculate members

**User Story:**
> As a user, I want to edit Bundle details and membership so that corrections propagate to every Bundle Option.

**Details:**
* **Priority:** 4
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Editing validates the same fields and minimum membership as creation
* [ ] Name, URL, price, regular price, membership, and Component label changes propagate transactionally
* [ ] Existing members retain position; newly added members append in checklist order
* [ ] Price or membership changes recalculate every share and preserve the exact Bundle total
* [ ] Removing a member deletes its generated Bundle Option and Option Comments while preserving Bundle Comments
* [ ] Editing any field on a chosen Bundle first unchooses every member Option and marks every member Item needed
* [ ] Failed edits leave the Bundle, Options, Comments, allocations, and Item Statuses unchanged
* [ ] Tests pass
* [ ] Typecheck passes

### US-005: Delete Bundles and handle Item deletion

**User Story:**
> As a user, I want Bundle deletion and Item deletion to leave no broken memberships or Options.

**Details:**
* **Priority:** 5
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Deleting a Bundle transactionally removes its generated Bundle Options, their Option Comments, its members, and its Bundle Comments
* [ ] Deleting a chosen Bundle first marks every member Item needed
* [ ] Deleting a member Item removes that membership and generated Option
* [ ] Deleting an Item from a chosen Bundle first unchooses the whole Bundle and marks its remaining members needed
* [ ] A surviving Bundle is reallocated after Item deletion; a Bundle with fewer than two remaining members is deleted
* [ ] Unrelated Bundles, Options, Comments, and Items remain unchanged
* [ ] Tests pass
* [ ] Typecheck passes

### US-006: Choose Bundles atomically

**User Story:**
> As a user, I want choosing one Bundle Option to purchase the whole Bundle so that every included Item stays consistent.

**Details:**
* **Priority:** 6
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Choosing a Bundle marks all its member Options chosen and all member Items bought in one transaction
* [ ] Existing chosen normal Options on target member Items are cleared
* [ ] Every chosen overlapping Bundle that conflicts through a target Item is fully unchosen, including members outside the target Bundle
* [ ] Displaced Bundle members become needed before the target Bundle members become bought
* [ ] Multiple conflicting Bundles can be displaced in one choice operation
* [ ] Replaced choices are not restored or retained as history
* [ ] A failed operation leaves all Option choices and Item Statuses unchanged
* [ ] Tests pass
* [ ] Typecheck passes

### US-007: Integrate normal Option and Item actions with chosen Bundles

**User Story:**
> As a user, I want normal Option and Item actions to respect an atomic Bundle purchase.

**Details:**
* **Priority:** 7
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Choosing a generated Bundle Option delegates to the whole-Bundle choice operation
* [ ] Choosing a normal Option on a chosen Bundle member unchooses the whole Bundle, marks its other members needed, then chooses the normal Option and marks its Item bought
* [ ] Marking one chosen Bundle member needed unchooses the whole Bundle and marks every member needed
* [ ] Marking an unchosen member Item bought directly does not choose its Bundle
* [ ] Existing behavior for Items and Options unrelated to chosen Bundles remains unchanged
* [ ] Tests pass
* [ ] Typecheck passes

### US-008: Render the production Bundle rail

**User Story:**
> As a user, I want a compact Bundle rail beside my checklist so that packages remain visible without obscuring Items.

**Details:**
* **Priority:** 8
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] The selected Option C layout renders Bundles in a left rail beside the checklist on desktop and above it on mobile
* [ ] Bundle cards default to compact summaries and expand to show members and actions
* [ ] Each card shows name, Bundle price, URL, member count, chosen state, and member Component labels with allocated shares
* [ ] When regular price exists, the card shows regular price plus savings amount and nearest whole percentage
* [ ] Cards render in Bundle creation order and members in stable position order
* [ ] The rail and checklist are inside the fragment refreshed by whole-list mutations
* [ ] The throwaway `?variant=A|B|C` switcher, prototype templates, prototype styles, and prototype-only server fields are removed
* [ ] The normal page remains usable with no Bundles
* [ ] HTTP integration tests pass
* [ ] browser tests pass
* [ ] Typecheck passes

### US-009: Create Bundles from the rail

**User Story:**
> As a user, I want a collapsed creation form in the Bundle rail so that I can map a store package to checklist Items.

**Details:**
* **Priority:** 9
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] The rail exposes an Add Bundle control and a collapsed form for name, URL, Bundle price, and optional regular price
* [ ] The member selector groups Items by Category in checklist order and shows each Item's bought/needed Status and current chosen Option
* [ ] Each selected Item exposes an optional Component label field
* [ ] The form requires at least two selected Items and explains that allocation is equal per Item
* [ ] Creation does not scrape the URL or add image, stock, preorder, dispatch, or shipping fields
* [ ] `POST /bundles` creates an unchosen Bundle and returns the refreshed rail and checklist fragment
* [ ] Validation failures return HTTP 400 with specific messages and do not create partial data
* [ ] User-supplied values are HTML escaped
* [ ] HTTP integration tests pass
* [ ] browser tests pass
* [ ] Typecheck passes

### US-010: Edit and delete Bundles from the rail

**User Story:**
> As a user, I want to edit or delete a Bundle from its card so that package details remain accurate.

**Details:**
* **Priority:** 10
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Bundle cards expose edit and delete controls with forms populated from current data
* [ ] `POST /bundles/{bid}` updates the Bundle and returns the refreshed rail and checklist fragment
* [ ] Saving an edit warns that a chosen Bundle will be unchosen and names affected Items
* [ ] Removing a member warns that its Bundle Option and Option Comments will be deleted
* [ ] `POST /bundles/{bid}/delete` names affected Items, requires confirmation, and refreshes the rail and checklist
* [ ] Missing or malformed Bundle IDs return HTTP 404
* [ ] HTTP integration tests pass
* [ ] browser tests pass
* [ ] Typecheck passes

### US-011: Render and choose Bundle Options under Items

**User Story:**
> As a user, I want each Bundle listed as an Option under its member Items so that I can compare it with normal Options.

**Details:**
* **Priority:** 11
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] An open Item lists each generated Bundle Option in existing Option insertion order beside normal Options
* [ ] A Bundle Option shows a Bundle badge, Component label or Item-name fallback, Bundle URL, member share, Bundle total, and chosen state
* [ ] Bundle Options have no individual delete control
* [ ] Item rows count Bundle Options and use the term Option rather than Link
* [ ] Choosing from the Bundle rail or any generated Bundle Option calls `POST /bundles/{bid}/choose`
* [ ] A destructive choice confirmation names every replaced normal Option, conflicting Bundle, and affected Item
* [ ] Choosing a normal Option that breaks a Bundle also confirms the full affected Bundle and Item list
* [ ] Choice mutations return the refreshed rail and checklist fragment
* [ ] HTTP integration tests pass
* [ ] browser tests pass
* [ ] Typecheck passes

### US-012: Show Bundle and Option Comments together

**User Story:**
> As a user, I want shared Bundle Comments and Item-specific Option Comments visible together so that both kinds of reasoning remain available.

**Details:**
* **Priority:** 12
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Expanded Bundle cards show the Bundle Comment thread and add/delete controls
* [ ] Every generated Bundle Option shows separate labelled Bundle Comments and Option Comments sections
* [ ] `POST /bundles/{bid}/comments` adds one shared Bundle Comment visible from the rail and every generated Bundle Option
* [ ] `POST /bundle-comments/{cid}/delete` removes only that Bundle Comment
* [ ] Existing Option Comment routes remain Item-specific and continue to work for Bundle Options
* [ ] Bundle Comment mutations refresh every visible copy without a page reload
* [ ] Blank Comments return HTTP 400 and user-supplied bodies are HTML escaped
* [ ] HTTP integration tests pass
* [ ] browser tests pass
* [ ] Typecheck passes

### US-013: Include Bundle allocations in budget summaries

**User Story:**
> As a user, I want a chosen Bundle reflected in Actual spend so that Category and whole-list totals remain exact.

**Details:**
* **Priority:** 13
* **Passes:** `false`
* **Notes:** *None*

**Acceptance Criteria:**
* [ ] Each bought member Item contributes its allocated Bundle Option price to its Category Actual spend
* [ ] Cross-Category member Actual spends sum to the exact Bundle price in the whole-list summary
* [ ] A `$100.00` Bundle split across three Items records `$33.34`, `$33.33`, and `$33.33`
* [ ] Unchosen Bundles do not affect Actual spend or replace needed Item Budgets
* [ ] Unchoosing, editing, deleting, or displacing a chosen Bundle removes its allocations from Actual spend
* [ ] HTTP integration tests pass
* [ ] Tests pass
* [ ] Typecheck passes
