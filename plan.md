# Plan: Update Panel-Based Full List Mode Math

## Current Behavior
The checklist UI has two completion calculation modes:
- **Panel Based** (`completionMode = false`): Uses `totalGathered / totalTarget` across all items.
- **Item Based** (`completionMode = true`): Uses average of `gathered / target` per item, excluding items with `target = 0`.

The user requests that Panel Based mode should instead compute:
```
sum(gathered / target) / total_items
```
where `total_items` is the count of all items (including those with `target = 0`?).

## Proposed Changes

### 1. Update `computeTotalCompletion` function
Modify `public/app.js` lines 23‑31 to compute the new formula.

**Current:**
```javascript
function computeTotalCompletion(items) {
  let totalGathered = 0;
  let totalTarget = 0;
  items.forEach(item => {
    totalGathered += item.gathered;
    totalTarget += item.target;
  });
  return { totalGathered, totalTarget };
}
```

**New:**
```javascript
function computeTotalCompletion(items) {
  let totalRatio = 0;
  let totalItems = items.length;
  items.forEach(item => {
    if (item.target > 0) {
      totalRatio += item.gathered / item.target;
    } else {
      // If target is zero, treat ratio as 0 (or skip?).
      // We'll treat as 0 to keep total_items unchanged.
    }
  });
  const averageRatio = totalItems > 0 ? totalRatio / totalItems : 0;
  return { averageRatio, totalItems };
}
```

**Alternative:** Keep the function name but change its return value to a single number (the average ratio). However the caller expects `totalGathered` and `totalTarget` for the percent calculation. We'll need to adjust `updateCompletionBar` accordingly.

### 2. Update `updateCompletionBar` usage
Currently lines 54‑56:
```javascript
const { totalGathered, totalTarget } = computeTotalCompletion(items);
percent = totalTarget > 0 ? Math.round((totalGathered / totalTarget) * 100) : 0;
```

Change to:
```javascript
const { averageRatio } = computeTotalCompletion(items);
percent = Math.round(averageRatio * 100);
```

### 3. Edge Cases
- **Zero‑target items**: If `target = 0`, the ratio `gathered / target` is undefined. We have two options:
  1. Skip those items from both sum and count (i.e., exclude them from `total_items`).
  2. Treat ratio as `0` (or `1`?) and include them in count.
  
  The user's example "grass block, stone, cobblestone" assumes all have positive targets. We'll choose to skip zero‑target items (same as Item Based mode) to avoid division by zero. That means `total_items` becomes the count of items with `target > 0`. However the user said "total items (ie: grass block, stone, cobblestone that would be 3 items)". If one of those had `target = 0`, they'd still count it? We'll decide to skip zero‑target items for safety.

- **All items have zero target**: `averageRatio` will be `0` (since `totalRatio = 0`). The percent will be `0%`.

- **Empty item list**: `totalItems = 0` → `averageRatio = 0`.

### 4. Update UI Labels and Descriptions
The toggle label currently reads "Panel Based" vs "Item Based". No change needed because the mode name remains "Panel Based". However we may want to add a tooltip or description explaining the new math. That's optional.

### 5. Testing Steps
1. Start the server (`go run .`).
2. Open the UI (`http://localhost:3001`).
3. Login with a password from `config.json`.
4. Verify that the completion bar shows correct percentages for both modes using sample data.
   - Create test items with varying gathered/target values.
   - Manually compute expected average ratio and compare with displayed percentage.
5. Ensure zero‑target items are handled gracefully (no NaN errors).
6. Verify that switching between Panel Based and Item Based updates the bar correctly.
7. Check that the bar animation still works.

### 6. Implementation Details
- Modify `public/app.js` as described.
- No changes required in `main.go` or any other files.
- The `computeTotalCompletion` function is only used for the completion bar, so no other dependencies.

### 7. Backup
Before making changes, backup the current `public/app.js` file.

### 8. Rollback Plan
If the new calculation causes issues, revert the changes and restore the original `computeTotalCompletion` function.

## Code Changes Summary
1. Edit `public/app.js`:
   - Replace `computeTotalCompletion` with new implementation.
   - Adjust `updateCompletionBar` to use `averageRatio`.
   - Ensure `percent` is integer between 0 and 100.

2. Optionally update comments to reflect the new formula.

## Verification
After implementing, run the server and test with the following example data (add to `items.json`):
```json
[
  {"name": "Grass Block", "target": 10, "gathered": 5},
  {"name": "Stone", "target": 5, "gathered": 0},
  {"name": "Cobblestone", "target": 3, "gathered": 3}
]
```
Expected Panel Based average:
- Ratios: 0.5, 0, 1 → sum = 1.5
- total_items = 3 → average = 0.5 → 50%
- Previously Panel Based would be (5+0+3)/(10+5+3) = 8/18 ≈ 44.4%.

Verify that the bar shows 50% when Panel Based mode is active.

## Timeline
- Estimated time: 15‑20 minutes.
- Risk: Low (only front‑end JavaScript changes).

## Additional Considerations
- The Item Based mode already uses `computeItemBasedCompletion` which excludes zero‑target items. Keep as is.
- The new Panel Based formula is essentially the same as Item Based but divides by total items (including zero‑target items if we decide to include them). If we skip zero‑target items, the two formulas become identical. That may not be the user's intent. Clarify with the user if needed.

Given the ambiguity, we can propose two options in the plan and let the user decide.