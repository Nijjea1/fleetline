// One palette for the map and the panel, so a color means the same thing everywhere.
// Vehicle status colors are reserved for vehicles; charts use neutral white/grey instead
// (an orange "Uber/Lyft" line next to amber "to pickup" cars would read as related).
export type RGB = [number, number, number];

export const STATUS = {
  idle: { code: 1, label: "Idle, waiting for a ride", short: "Idle", rgb: [110, 168, 254] as RGB },
  pickup: { code: 2, label: "Driving to a pickup", short: "To pickup", rgb: [255, 180, 84] as RGB },
  trip: { code: 3, label: "Carrying a rider", short: "On trip", rgb: [61, 220, 151] as RGB },
} as const;

export const STATUS_BY_CODE: Record<number, (typeof STATUS)[keyof typeof STATUS]> = {
  1: STATUS.idle,
  2: STATUS.pickup,
  3: STATUS.trip,
};

export const RIDER: RGB = [255, 93, 115];
export const ROBOTAXI: RGB = [192, 132, 252];

export const css = (c: RGB, a = 1) => `rgba(${c[0]}, ${c[1]}, ${c[2]}, ${a})`;
