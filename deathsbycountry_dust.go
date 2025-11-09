package main

import (
    "fmt"
	"os"
	"strconv"
    "strings"
	"sync"
	"database/sql"
	"flag"
    "github.com/ctessum/geom/index/rtree"
	"github.com/ctessum/geom"
	"github.com/ctessum/geom/encoding/shp"
	"github.com/ctessum/geom/encoding/wkb"
	jshp "github.com/jonas-p/go-shp"
    "math"
	_ "github.com/mattn/go-sqlite3"
//	"github.com/fhs/go-netcdf/netcdf"
//	"github.com/spatialmodel/inmap"
)

var (
	mode         = flag.String("mode", "direct", "Mode: 'create-mapping', 'apply-mapping', or 'direct' (default)")
	inputFile    = flag.String("input", "", "Path to input shapefile with deaths data (required for direct/apply-mapping mode)")
	outputFile   = flag.String("output", "deaths_by_country.shp", "Path to output shapefile")
	countryFile  = flag.String("countries", "ee_r250_correspondence.gpkg", "Path to country boundaries GeoPackage file")
	fieldName    = flag.String("field", "TotalPopD", "Field name in input shapefile containing death values")
	inmapGrid    = flag.String("inmap-grid", "", "Path to InMAP grid shapefile (required for create-mapping mode)")
	mappingFile  = flag.String("mapping", "inmap_country_mapping.csv", "Path to mapping file (create or read)")
)

func main(){
    flag.Parse()

    switch *mode {
    case "create-mapping":
        createMapping()
    case "apply-mapping":
        applyMapping()
    case "direct":
        directAggregation()
    default:
        fmt.Printf("Error: unknown mode '%s'. Must be 'create-mapping', 'apply-mapping', or 'direct'\n", *mode)
        flag.PrintDefaults()
    }
}

// createMapping computes the intersection matrix once and saves it
func createMapping() {
    // Validate required flags
    if *inmapGrid == "" || *countryFile == "" {
        fmt.Println("Error: -inmap-grid and -countries flags are required for create-mapping mode")
        fmt.Println("\nUsage:")
        flag.PrintDefaults()
        return
    }

    fmt.Println("=== Creating Mapping ===")
    fmt.Printf("InMAP grid: %s\n", *inmapGrid)
    fmt.Printf("Country file: %s\n", *countryFile)
    fmt.Printf("Output mapping: %s\n", *mappingFile)
    fmt.Println("\nReading geometries...")

    // Read InMAP grid (just geometries, don't need IDs)
    inmapCells := getGeometries(*inmapGrid)
    fmt.Printf("Loaded %d InMAP cells\n", len(inmapCells))

    // Read country geometries
    countryShapes := getGeometriesGpkg(*countryFile)
    fmt.Printf("Loaded %d countries\n", len(countryShapes))

    fmt.Println("\nComputing intersection mapping (this may take a while)...")
    mapping := computeMapping(inmapCells, nil, countryShapes, nil)

    fmt.Printf("Computed %d intersection records\n", len(mapping))
    fmt.Printf("Saving mapping to %s...\n", *mappingFile)
    saveMapping(mapping, *mappingFile)

    fmt.Println("Done! Mapping saved successfully.")
}

// applyMapping uses a precomputed mapping for fast aggregation
func applyMapping() {
    // Validate required flags
    if *inputFile == "" {
        fmt.Println("Error: -input flag is required for apply-mapping mode")
        fmt.Println("\nUsage:")
        flag.PrintDefaults()
        return
    }

    fmt.Println("=== Applying Mapping ===")
    fmt.Printf("Input file: %s\n", *inputFile)
    fmt.Printf("Mapping file: %s\n", *mappingFile)
    fmt.Printf("Output file: %s\n", *outputFile)
    fmt.Printf("Field name: %s\n", *fieldName)

    fmt.Println("\nLoading mapping...")
    mapping := loadMapping(*mappingFile)
    fmt.Printf("Loaded %d intersection records\n", len(mapping))

    fmt.Println("Reading input data...")
    _, inmapData := getTots(*inputFile, *fieldName)
    fmt.Printf("Loaded %d data cells\n", len(inmapData))

    fmt.Println("Loading country geometries and names...")
    countryShapes, countryNames, countryFIDs := getGeometriesAndNamesGpkg(*countryFile)
    fmt.Printf("Loaded %d countries\n", len(countryShapes))

    fmt.Println("Applying mapping...")
    countryData := applyMappingToData(mapping, inmapData)

    fmt.Println("Writing output...")
    writeTotDeathsWithNames(countryShapes, countryData, countryNames, countryFIDs, *outputFile)

    fmt.Printf("\nDone! Output written to: %s\n", *outputFile)
}

// directAggregation performs the full computation without mapping (original behavior)
func directAggregation() {
    // Validate required flags
    if *inputFile == "" {
        fmt.Println("Error: -input flag is required")
        fmt.Println("\nUsage:")
        flag.PrintDefaults()
        return
    }

    fmt.Printf("Input file: %s\n", *inputFile)
    fmt.Printf("Output file: %s\n", *outputFile)
    fmt.Printf("Country file: %s\n", *countryFile)
    fmt.Printf("Field name: %s\n", *fieldName)
    fmt.Println("\nStarting aggregation...")

    inmapCells, attrib          := getTots(*inputFile, *fieldName)
    countryShapes, _            := getGpkgData(*countryFile, "fid")
    rattrib, err                := regridSum(inmapCells, countryShapes, attrib)
    check(err)
    writeTotDeaths(countryShapes, rattrib, *outputFile)

    fmt.Printf("\nDone! Output written to: %s\n", *outputFile)
}


// MappingRecord represents one entry in the sparse intersection matrix
type MappingRecord struct {
    InmapCellIndex  int     // Index of InMAP cell
    CountryIndex    int     // Index of country
    Fraction        float64 // What fraction of InMAP cell data goes to this country
}

// computeMapping creates the sparse intersection matrix (parallelized)
func computeMapping(inmapCells []geom.Polygonal, inmapIDs []float64, countryCells []geom.Polygonal, countryIDs []float64) []MappingRecord {
    type data struct {
        geom.Polygonal
        index int
        area  float64
    }

    // Build spatial index for InMAP cells
    fmt.Println("Building spatial index...")
    index := rtree.NewTree(25, 50)
    for i, g := range inmapCells {
        index.Insert(&data{
            Polygonal: g,
            index:     i,
            area:      g.Area(),
        })
    }

    // Process countries in parallel
    fmt.Println("Computing intersections in parallel...")
    type countryRecords struct {
        countryIdx int
        records    []MappingRecord
    }

    resultsChan := make(chan countryRecords, len(countryCells))
    var wg sync.WaitGroup

    // Semaphore to limit concurrent goroutines (avoid overwhelming the system)
    maxConcurrent := 8
    sem := make(chan struct{}, maxConcurrent)

    for countryIdx, countryGeom := range countryCells {
        wg.Add(1)
        sem <- struct{}{} // Acquire semaphore

        go func(idx int, geom geom.Polygonal) {
            defer wg.Done()
            defer func() { <-sem }() // Release semaphore

            if idx%10 == 0 {
                fmt.Printf("  Processing country %d of %d\n", idx, len(countryCells))
            }

            var localRecords []MappingRecord

            for _, dI := range index.SearchIntersect(geom.Bounds()) {
                d := dI.(*data)
                isect := geom.Intersection(d.Polygonal)
                if isect == nil {
                    continue
                }
                intersectionArea := isect.Area()
                fraction := intersectionArea / d.area

                if fraction > 0 {
                    localRecords = append(localRecords, MappingRecord{
                        InmapCellIndex: d.index,
                        CountryIndex:   idx,
                        Fraction:       fraction,
                    })
                }
            }

            resultsChan <- countryRecords{countryIdx: idx, records: localRecords}
        }(countryIdx, countryGeom)
    }

    // Close results channel when all goroutines complete
    go func() {
        wg.Wait()
        close(resultsChan)
    }()

    // Collect all records
    var allRecords []MappingRecord
    for result := range resultsChan {
        allRecords = append(allRecords, result.records...)
    }

    return allRecords
}

// saveMapping writes the mapping to a CSV file
func saveMapping(records []MappingRecord, filename string) {
    file, err := os.Create(filename)
    check(err)
    defer file.Close()

    // Write header
    _, err = file.WriteString("inmap_cell_index,country_index,fraction\n")
    check(err)

    // Write records
    for _, r := range records {
        _, err = file.WriteString(fmt.Sprintf("%d,%d,%.15f\n", r.InmapCellIndex, r.CountryIndex, r.Fraction))
        check(err)
    }
}

// loadMapping reads the mapping from a CSV file
func loadMapping(filename string) []MappingRecord {
    data, err := os.ReadFile(filename)
    check(err)

    var records []MappingRecord
    lines := strings.Split(string(data), "\n")

    for i, line := range lines {
        if i == 0 || line == "" {
            continue // Skip header and empty lines
        }

        parts := strings.Split(line, ",")
        if len(parts) != 3 {
            continue
        }

        inmapIdx, err := strconv.Atoi(parts[0])
        check(err)
        countryIdx, err := strconv.Atoi(parts[1])
        check(err)
        fraction, err := strconv.ParseFloat(parts[2], 64)
        check(err)

        records = append(records, MappingRecord{
            InmapCellIndex: inmapIdx,
            CountryIndex:   countryIdx,
            Fraction:       fraction,
        })
    }

    return records
}

// applyMappingToData uses the precomputed mapping to aggregate data
func applyMappingToData(mapping []MappingRecord, inmapData []float64) []float64 {
    // Find max country index to size the output array
    maxCountryIdx := 0
    for _, r := range mapping {
        if r.CountryIndex > maxCountryIdx {
            maxCountryIdx = r.CountryIndex
        }
    }

    countryData := make([]float64, maxCountryIdx+1)

    // Apply the mapping: for each record, add (inmap_data * fraction) to the country
    for _, r := range mapping {
        if r.InmapCellIndex < len(inmapData) {
            countryData[r.CountryIndex] += inmapData[r.InmapCellIndex] * r.Fraction
        }
    }

    return countryData
}

func GEMM(z, θ, α, μ, v float64) (float64) {
    z       =       math.Max(z-2.4,0)
    denom   :=      1.0 + math.Exp(-(z-μ)/v)
    numer   :=      θ * math.Log((z/α)+1)
    return math.Exp(numer/denom)
}

//func readGBD() ([]float64) {

//}

//func assignToStates(df ???, stateVals []string, mapping ???) (gbdVals []string) {
//}

func writeShpData(cells []geom.Polygonal, native, regridded []float64) {
	type shpOut struct {
		geom.Polygon
		Native, Regridded, Diff float64
	}

	e, err := shp.NewEncoder("regridded-states-to-InMAP-Cells.shp", shpOut{})
	check(err)
	for i, c := range cells {
		check(e.Encode(shpOut{
			Polygon:   c.Polygons()[0], // Need to change if ever using a multipolygon here.
			Native:    native[i],
			Regridded: regridded[i],
			Diff:      regridded[i] - native[i],
		}))
	}
	e.Close()
}

func writeTotDeaths(cells []geom.Polygonal, native []float64, filename string) {
	type shpOut struct {
		geom.Polygon
		RRs float64
	}

	e, err := shp.NewEncoder(filename, shpOut{})
	check(err)
	for i, c := range cells {
		check(e.Encode(shpOut{
			Polygon:   c.Polygons()[0], // Need to change if ever using a multipolygon here.
			RRs:    native[i],
		}))
	}
	e.Close()
}

// writeTotDeathsWithNames writes output shapefile with country names and fids using jonas-p/go-shp
func writeTotDeathsWithNames(cells []geom.Polygonal, native []float64, names []string, fids []int, filename string) {
	// Create shapefile
	shape, err := jshp.Create(filename, jshp.POLYGON)
	check(err)
	defer shape.Close()

	// Add attribute fields
	shape.SetFields([]jshp.Field{
		jshp.NumberField("FID", 9),
		jshp.StringField("Country", 80),
		jshp.FloatField("Deaths", 19, 11),
	})

	for i, c := range cells {
		countryName := ""
		countryFID := 0
		if i < len(names) {
			countryName = names[i]
		}
		if i < len(fids) {
			countryFID = fids[i]
		}

		// Convert geom.Polygonal to jonas-p/go-shp Polygon
		// Get all the polygon parts
		polygons := c.Polygons()

		// Convert to [][]Point format expected by NewPolyLine
		var parts [][]jshp.Point
		for _, poly := range polygons {
			// Each geom.Polygon is a slice of geom.Path (outer ring + holes)
			for _, ring := range poly {
				var ringPoints []jshp.Point
				// Convert each point in the ring
				for _, pt := range ring {
					ringPoints = append(ringPoints, jshp.Point{
						X: pt.X,
						Y: pt.Y,
					})
				}
				parts = append(parts, ringPoints)
			}
		}

		// Create the polygon shape (Polygon is an alias for PolyLine)
		polyLine := jshp.NewPolyLine(parts)
		shpPoly := jshp.Polygon(*polyLine)

		// Write the shape and attributes
		shape.Write(&shpPoly)
		shape.WriteAttribute(i, 0, countryFID)
		shape.WriteAttribute(i, 1, countryName)
		shape.WriteAttribute(i, 2, native[i])
	}
}

func writeOutCountries(cells []geom.Polygonal, native []float64, filename string, countryName []float64) {
	type shpOut struct {
		geom.Polygon
		Deaths float64
        Country float64
	}

	e, err := shp.NewEncoder(filename, shpOut{})
	check(err)
	for i, c := range cells {
		check(e.Encode(shpOut{
			Polygon:   c.Polygons()[0], // Need to change if ever using a multipolygon here.
			Deaths:    native[i],
            Country:   countryName[i],
		}))
	}
	e.Close()
}


// Handle errors
func check(err error) {
	if err != nil {
		panic(err)
	}
}

// Getting the state data (strings).
func getStateData(shpFile, pol string) ([]geom.Polygonal, []string) {
	s, err := shp.NewDecoder(shpFile)
	check(err)

	var data []string
	var cells []geom.Polygonal
	for {
		g, fields, more := s.DecodeRowFields(pol)
		if !more {
			break
		}
		v := fields[pol]
		cells = append(cells, g.(geom.Polygonal))
		data = append(data, v)

	}
	s.Close()
	check(s.Error())
	return cells, data
}

// getGeometries reads just the geometries from a shapefile (no field data)
func getGeometries(shpFile string) []geom.Polygonal {
	s, err := shp.NewDecoder(shpFile)
	check(err)
	defer s.Close()

	var cells []geom.Polygonal
	for {
		// DecodeRowFields with no fields still gives us the geometry
		g, _, more := s.DecodeRowFields()
		if !more {
			break
		}
		cells = append(cells, g.(geom.Polygonal))
	}
	check(s.Error())
	return cells
}

// getGeometriesGpkg reads just the geometries from a GeoPackage (no field data)
func getGeometriesGpkg(gpkgFile string) []geom.Polygonal {
	db, err := sql.Open("sqlite3", gpkgFile)
	check(err)
	defer db.Close()

	// Find the table name
	var tableName string
	err = db.QueryRow("SELECT table_name FROM gpkg_contents WHERE data_type = 'features' LIMIT 1").Scan(&tableName)
	check(err)

	// Find the geometry column name
	var geomColumn string
	err = db.QueryRow("SELECT column_name FROM gpkg_geometry_columns WHERE table_name = ?", tableName).Scan(&geomColumn)
	check(err)

	// Query just the geometries
	query := fmt.Sprintf("SELECT %s FROM %s", geomColumn, tableName)
	rows, err := db.Query(query)
	check(err)
	defer rows.Close()

	var cells []geom.Polygonal

	for rows.Next() {
		var geomBytes []byte
		err := rows.Scan(&geomBytes)
		check(err)

		// Skip GeoPackage header
		var wkbData []byte
		if len(geomBytes) > 8 && geomBytes[0] == 'G' && geomBytes[1] == 'P' {
			flags := geomBytes[3]
			headerSize := 8
			envelopeType := (flags >> 1) & 0x07
			switch envelopeType {
			case 1:
				headerSize += 32
			case 2:
				headerSize += 48
			case 3:
				headerSize += 48
			case 4:
				headerSize += 64
			}
			wkbData = geomBytes[headerSize:]
		} else {
			wkbData = geomBytes
		}

		g, err := wkb.Decode(wkbData)
		check(err)

		if poly, ok := g.(geom.Polygonal); ok {
			cells = append(cells, poly)
		}
	}

	check(rows.Err())
	return cells
}

// getGeometriesAndNamesGpkg reads geometries, country names, and fids from a GeoPackage
func getGeometriesAndNamesGpkg(gpkgFile string) ([]geom.Polygonal, []string, []int) {
	db, err := sql.Open("sqlite3", gpkgFile)
	check(err)
	defer db.Close()

	// Find the table name
	var tableName string
	err = db.QueryRow("SELECT table_name FROM gpkg_contents WHERE data_type = 'features' LIMIT 1").Scan(&tableName)
	check(err)

	// Find the geometry column name
	var geomColumn string
	err = db.QueryRow("SELECT column_name FROM gpkg_geometry_columns WHERE table_name = ?", tableName).Scan(&geomColumn)
	check(err)

	// Query geometries, country names, and fids
	query := fmt.Sprintf("SELECT %s, iso3_r250_name, fid FROM %s ORDER BY fid", geomColumn, tableName)
	rows, err := db.Query(query)
	check(err)
	defer rows.Close()

	var cells []geom.Polygonal
	var names []string
	var fids []int

	for rows.Next() {
		var geomBytes []byte
		var name string
		var fid int
		err := rows.Scan(&geomBytes, &name, &fid)
		check(err)

		// Skip GeoPackage header
		var wkbData []byte
		if len(geomBytes) > 8 && geomBytes[0] == 'G' && geomBytes[1] == 'P' {
			flags := geomBytes[3]
			headerSize := 8
			envelopeType := (flags >> 1) & 0x07
			switch envelopeType {
			case 1:
				headerSize += 32
			case 2:
				headerSize += 48
			case 3:
				headerSize += 48
			case 4:
				headerSize += 64
			}
			wkbData = geomBytes[headerSize:]
		} else {
			wkbData = geomBytes
		}

		g, err := wkb.Decode(wkbData)
		check(err)

		if poly, ok := g.(geom.Polygonal); ok {
			cells = append(cells, poly)
			names = append(names, name)
			fids = append(fids, fid)
		}
	}

	check(rows.Err())
	return cells, names, fids
}

// Read shapefile data
func getTots(shpFile, pol string) ([]geom.Polygonal, []float64) {
	s, err := shp.NewDecoder(shpFile)
	check(err)

	var data []float64
	var cells []geom.Polygonal
	for {
		g, fields, more := s.DecodeRowFields(pol)
		if !more {
			break
		}
        mm := strings.Replace(fields[pol]," ","",-1)
        v, err := strconv.ParseFloat(strings.Replace(mm,"\x00", "", -1),64)
        check(err)
		cells = append(cells, g.(geom.Polygonal))
		data = append(data, v)
	}
	s.Close()
	check(s.Error())
	return cells, data
}

func getShpData(shpFile, pol string) ([]geom.Polygonal, []float64) {
	s, err := shp.NewDecoder(shpFile)
	check(err)

	var data []float64
	var cells []geom.Polygonal
	for {
		g, fields, more := s.DecodeRowFields(pol)
		if !more {
			break
		}
		v, err := strconv.ParseFloat(fields[pol], 64)
		check(err)
		cells = append(cells, g.(geom.Polygonal))
		data = append(data, v)

	}
	s.Close()
	check(s.Error())
	return cells, data
}

// Read GeoPackage data
func getGpkgData(gpkgFile, fieldName string) ([]geom.Polygonal, []float64) {
	db, err := sql.Open("sqlite3", gpkgFile)
	check(err)
	defer db.Close()

	// Find the table name - GeoPackages typically have a gpkg_contents table
	var tableName string
	err = db.QueryRow("SELECT table_name FROM gpkg_contents WHERE data_type = 'features' LIMIT 1").Scan(&tableName)
	check(err)

	// Find the geometry column name
	var geomColumn string
	err = db.QueryRow("SELECT column_name FROM gpkg_geometry_columns WHERE table_name = ?", tableName).Scan(&geomColumn)
	check(err)

	// Query the data
	query := fmt.Sprintf("SELECT %s, %s FROM %s", geomColumn, fieldName, tableName)
	rows, err := db.Query(query)
	check(err)
	defer rows.Close()

	var data []float64
	var cells []geom.Polygonal

	for rows.Next() {
		var geomBytes []byte
		var value float64
		err := rows.Scan(&geomBytes, &value)
		check(err)

		// GeoPackage geometry is stored with a header, need to skip it
		// The header is variable length but starts with 'GP' magic bytes
		// Standard header is 8 bytes minimum
		var wkbData []byte
		if len(geomBytes) > 8 && geomBytes[0] == 'G' && geomBytes[1] == 'P' {
			// Skip GeoPackage header (8 bytes for standard header)
			// Byte 3 is flags, byte 4-7 is SRID
			flags := geomBytes[3]
			headerSize := 8
			// Check envelope flags (bits 1-3)
			envelopeType := (flags >> 1) & 0x07
			switch envelopeType {
			case 1: // XY envelope
				headerSize += 32
			case 2: // XYZ envelope
				headerSize += 48
			case 3: // XYM envelope
				headerSize += 48
			case 4: // XYZM envelope
				headerSize += 64
			}
			wkbData = geomBytes[headerSize:]
		} else {
			wkbData = geomBytes
		}

		g, err := wkb.Decode(wkbData)
		check(err)

		if poly, ok := g.(geom.Polygonal); ok {
			cells = append(cells, poly)
			data = append(data, value)
		}
	}

	check(rows.Err())
	return cells, data
}


// Regrid regrids concentration data from one spatial grid to a
// different one.
func regrid(oldGeom, newGeom []geom.Polygonal, oldData []float64) (newData []float64, err error) {
    type data struct {
        geom.Polygonal
        data float64
    }
    if len(oldGeom) != len(oldData) {
        return nil, fmt.Errorf("oldGeom and oldData have different lengths: %d!=%d", len(oldGeom), len(oldData))
    }
    index := rtree.NewTree(25, 50)
    for i, g := range oldGeom {
        index.Insert(&data{
            Polygonal: g,
            data:      oldData[i],
        })
    }
    newData = make([]float64, len(newGeom))
    for i, g := range newGeom {
        for _, dI := range index.SearchIntersect(g.Bounds()) {
            d := dI.(*data)
            isect := g.Intersection(d.Polygonal)
            if isect == nil {
                continue
            }
            a := isect.Area()
            frac := a / g.Area()
            newData[i] += d.data * frac
        }
    }
    return newData, nil
}

func regridSum(oldGeom, newGeom []geom.Polygonal, oldData []float64) (newData []float64, err error) {
    type data struct {
        geom.Polygonal
        data float64
        area float64  // Cache the area
    }
    if len(oldGeom) != len(oldData) {
        return nil, fmt.Errorf("oldGeom and oldData have different lengths: %d!=%d", len(oldGeom), len(oldData))
    }

    // Pre-compute areas of old geometries
    fmt.Println("Pre-computing areas...")
    index := rtree.NewTree(25, 50)
    for i, g := range oldGeom {
        index.Insert(&data{
            Polygonal: g,
            data:      oldData[i],
            area:      g.Area(),  // Cache area once
        })
    }

    // Parallelize the regridding across countries
    fmt.Println("Regridding with parallel processing...")
    newData = make([]float64, len(newGeom))
    var wg sync.WaitGroup

    for i, g := range newGeom {
        wg.Add(1)
        go func(idx int, geom geom.Polygonal) {
            defer wg.Done()
            var sum float64
            for _, dI := range index.SearchIntersect(geom.Bounds()) {
                d := dI.(*data)
                isect := geom.Intersection(d.Polygonal)
                if isect == nil {
                    continue
                }
                a := isect.Area()
                frac := a / d.area  // Use cached area
                sum += d.data * frac
            }
            newData[idx] = sum
        }(i, g)
    }
    wg.Wait()

    return newData, nil
}
