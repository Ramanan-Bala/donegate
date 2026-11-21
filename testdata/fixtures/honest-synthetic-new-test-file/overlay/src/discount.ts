const BULK_DISCOUNT_THRESHOLD = 1000;
const BULK_DISCOUNT_RATE = 0.1;

export function applyDiscount(subtotal: number): number {
  if (subtotal >= BULK_DISCOUNT_THRESHOLD) {
    return subtotal * (1 - BULK_DISCOUNT_RATE);
  }
  return subtotal;
}
