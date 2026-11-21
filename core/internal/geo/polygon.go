package geo

// Polygon is a simple lon/lat ring (first point need not repeat at the end).
type Polygon [][2]float64 // [lon, lat]

// Contains uses ray casting: count how many edges a ray going east from the point
// crosses. Odd = inside. Fine for city-scale polygons that don't cross the antimeridian.
func (p Polygon) Contains(lat, lon float64) bool {
	in := false
	for i, j := 0, len(p)-1; i < len(p); j, i = i, i+1 {
		xi, yi := p[i][0], p[i][1]
		xj, yj := p[j][0], p[j][1]
		if (yi > lat) != (yj > lat) && lon < (xj-xi)*(lat-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// RobotaxiZone is an approximate outline of Manhattan below 110th Street. Robotaxi
// services launch inside a mapped, geofenced area; ours only accepts trips that both
// start and end inside it. Hand-traced, accurate to roughly a block.
var RobotaxiZone = Polygon{
	{-74.0190, 40.7000}, // Battery
	{-74.0160, 40.7250},
	{-74.0110, 40.7500},
	{-74.0000, 40.7650},
	{-73.9880, 40.7820},
	{-73.9670, 40.8045}, // W 110th St & Riverside
	{-73.9580, 40.8000}, // Central Park North
	{-73.9380, 40.7905}, // E 110th St & FDR
	{-73.9420, 40.7760},
	{-73.9590, 40.7570},
	{-73.9710, 40.7420},
	{-73.9750, 40.7270},
	{-73.9740, 40.7110},
	{-73.9970, 40.7070},
	{-74.0110, 40.7010},
}
