package geo

import (
	"encoding/json"
	"strconv"
	"strings"
)

// MarshalText emits a shortest round-trippable decimal longitude.
func (longitude Longitude) MarshalText() ([]byte, error) {
	return formatFloat(longitude.degrees), nil
}

// UnmarshalText parses and validates one decimal longitude.
func (longitude *Longitude) UnmarshalText(data []byte) error {
	value, err := parseFloat("longitude", data)
	if err != nil {
		return err
	}
	parsed, err := NewLongitude(value)
	if err != nil {
		return err
	}
	*longitude = parsed
	return nil
}

// MarshalJSON emits a finite JSON number.
func (longitude Longitude) MarshalJSON() ([]byte, error) {
	return longitude.MarshalText()
}

// UnmarshalJSON parses and validates one JSON number.
func (longitude *Longitude) UnmarshalJSON(data []byte) error {
	return unmarshalJSONFloat("longitude", data, func(value float64) error {
		parsed, err := NewLongitude(value)
		if err == nil {
			*longitude = parsed
		}
		return err
	})
}

// MarshalText emits a shortest round-trippable decimal latitude.
func (latitude Latitude) MarshalText() ([]byte, error) {
	return formatFloat(latitude.degrees), nil
}

// UnmarshalText parses and validates one decimal latitude.
func (latitude *Latitude) UnmarshalText(data []byte) error {
	value, err := parseFloat("latitude", data)
	if err != nil {
		return err
	}
	parsed, err := NewLatitude(value)
	if err != nil {
		return err
	}
	*latitude = parsed
	return nil
}

// MarshalJSON emits a finite JSON number.
func (latitude Latitude) MarshalJSON() ([]byte, error) {
	return latitude.MarshalText()
}

// UnmarshalJSON parses and validates one JSON number.
func (latitude *Latitude) UnmarshalJSON(data []byte) error {
	return unmarshalJSONFloat("latitude", data, func(value float64) error {
		parsed, err := NewLatitude(value)
		if err == nil {
			*latitude = parsed
		}
		return err
	})
}

// MarshalText emits a shortest round-trippable decimal altitude in metres.
func (altitude Altitude) MarshalText() ([]byte, error) {
	return formatFloat(altitude.metres), nil
}

// UnmarshalText parses and validates one altitude in metres.
func (altitude *Altitude) UnmarshalText(data []byte) error {
	value, err := parseFloat("altitude", data)
	if err != nil {
		return err
	}
	parsed, err := NewAltitudeMeters(value)
	if err != nil {
		return err
	}
	*altitude = parsed
	return nil
}

// MarshalJSON emits a finite JSON number in metres.
func (altitude Altitude) MarshalJSON() ([]byte, error) {
	return altitude.MarshalText()
}

// UnmarshalJSON parses and validates one JSON altitude in metres.
func (altitude *Altitude) UnmarshalJSON(data []byte) error {
	return unmarshalJSONFloat("altitude", data, func(value float64) error {
		parsed, err := NewAltitudeMeters(value)
		if err == nil {
			*altitude = parsed
		}
		return err
	})
}

// MarshalText emits a shortest round-trippable decimal bearing.
func (bearing Bearing) MarshalText() ([]byte, error) {
	return formatFloat(bearing.degrees), nil
}

// UnmarshalText parses and validates one decimal bearing.
func (bearing *Bearing) UnmarshalText(data []byte) error {
	value, err := parseFloat("bearing", data)
	if err != nil {
		return err
	}
	parsed, err := NewBearing(value)
	if err != nil {
		return err
	}
	*bearing = parsed
	return nil
}

// MarshalJSON emits a finite JSON number in degrees.
func (bearing Bearing) MarshalJSON() ([]byte, error) {
	return bearing.MarshalText()
}

// UnmarshalJSON parses and validates one JSON bearing in degrees.
func (bearing *Bearing) UnmarshalJSON(data []byte) error {
	return unmarshalJSONFloat("bearing", data, func(value float64) error {
		parsed, err := NewBearing(value)
		if err == nil {
			*bearing = parsed
		}
		return err
	})
}

// MarshalText emits a shortest round-trippable distance in metres.
func (distance Distance) MarshalText() ([]byte, error) {
	return formatFloat(distance.metres), nil
}

// UnmarshalText parses and validates one distance in metres.
func (distance *Distance) UnmarshalText(data []byte) error {
	value, err := parseFloat("distance", data)
	if err != nil {
		return err
	}
	parsed, err := NewDistanceMeters(value)
	if err != nil {
		return err
	}
	*distance = parsed
	return nil
}

// MarshalJSON emits a finite JSON number in metres.
func (distance Distance) MarshalJSON() ([]byte, error) {
	return distance.MarshalText()
}

// UnmarshalJSON parses and validates one JSON distance in metres.
func (distance *Distance) UnmarshalJSON(data []byte) error {
	return unmarshalJSONFloat("distance", data, func(value float64) error {
		parsed, err := NewDistanceMeters(value)
		if err == nil {
			*distance = parsed
		}
		return err
	})
}

// MarshalText uses srid:name so arbitrary CRS names remain explicit.
func (crs CRS) MarshalText() ([]byte, error) {
	if !crs.valid() {
		return nil, &CRSError{SRID: crs.srid, Problem: "cannot marshal invalid CRS"}
	}
	return []byte(strconv.FormatInt(int64(crs.srid), 10) + ":" + crs.name), nil
}

// UnmarshalText parses canonical "SRID:name" CRS text.
func (crs *CRS) UnmarshalText(data []byte) error {
	sridText, name, found := strings.Cut(string(data), ":")
	if !found || name == "" {
		return textEncodingError("CRS must use srid:name", nil)
	}
	srid, err := strconv.ParseInt(sridText, 10, 32)
	if err != nil {
		return textEncodingError("CRS has invalid SRID", err)
	}
	parsed, err := NewCRS(int32(srid), name)
	if err != nil {
		return err
	}
	*crs = parsed
	return nil
}

// UnmarshalJSON parses explicit SRID and name members.
func (crs *CRS) UnmarshalJSON(data []byte) error {
	var decoded struct {
		SRID int32  `json:"srid"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return jsonEncodingError("invalid CRS object", err)
	}
	parsed, err := NewCRS(decoded.SRID, decoded.Name)
	if err != nil {
		return err
	}
	*crs = parsed
	return nil
}

// MarshalText uses longitude,latitude@srid:name. Longitude always comes first.
func (coordinate Coordinate) MarshalText() ([]byte, error) {
	crs, err := coordinate.crs.MarshalText()
	if err != nil {
		return nil, err
	}
	result := formatFloat(coordinate.longitude.degrees)
	result = append(result, ',')
	result = append(result, formatFloat(coordinate.latitude.degrees)...)
	result = append(result, '@')
	result = append(result, crs...)
	return result, nil
}

// UnmarshalText parses canonical longitude, latitude, and CRS text.
func (coordinate *Coordinate) UnmarshalText(data []byte) error {
	position, crsText, found := strings.Cut(string(data), "@")
	if !found {
		return textEncodingError(
			"coordinate must use longitude,latitude@srid:name",
			nil,
		)
	}
	longitudeText, latitudeText, found := strings.Cut(position, ",")
	if !found || strings.Contains(latitudeText, ",") {
		return textEncodingError(
			"coordinate must contain exactly two values",
			nil,
		)
	}
	var longitude Longitude
	if err := longitude.UnmarshalText([]byte(longitudeText)); err != nil {
		return err
	}
	var latitude Latitude
	if err := latitude.UnmarshalText([]byte(latitudeText)); err != nil {
		return err
	}
	var crs CRS
	if err := crs.UnmarshalText([]byte(crsText)); err != nil {
		return err
	}
	*coordinate = Coordinate{
		longitude: longitude,
		latitude:  latitude,
		crs:       crs,
	}
	return nil
}

// UnmarshalJSON parses named longitude, latitude, and CRS members.
func (coordinate *Coordinate) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Longitude Longitude `json:"longitude"`
		Latitude  Latitude  `json:"latitude"`
		CRS       CRS       `json:"crs"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return jsonEncodingError("invalid coordinate object", err)
	}
	parsed, err := NewCoordinate(decoded.Longitude, decoded.Latitude, decoded.CRS)
	if err != nil {
		return err
	}
	*coordinate = parsed
	return nil
}

func formatFloat(value float64) []byte {
	return strconv.AppendFloat(nil, value, 'g', -1, 64)
}

func parseFloat(name string, data []byte) (float64, error) {
	value, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return 0, textEncodingError(name+" must be a number", err)
	}
	return value, nil
}

func unmarshalJSONFloat(
	name string,
	data []byte,
	assign func(float64) error,
) error {
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return jsonEncodingError(name+" must be a JSON number", err)
	}
	return assign(value)
}

func textEncodingError(problem string, cause error) error {
	return &EncodingError{Format: "text", Problem: problem, Cause: cause}
}

func jsonEncodingError(problem string, cause error) error {
	return &EncodingError{Format: "JSON", Problem: problem, Cause: cause}
}
