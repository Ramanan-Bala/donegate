import { afterEach, beforeEach, vi } from 'vitest';

beforeEach(() => {
  vi.spyOn(Math, 'round').mockReturnValue(90000);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});
