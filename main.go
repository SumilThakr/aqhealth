package main

import (
    "os"
    "fmt"
	"strconv"
    "path/filepath"
    "strings"
    "flag"
    "encoding/json"
    "io/ioutil"
    "github.com/ctessum/geom/index/rtree"
	"github.com/ctessum/geom"
	"github.com/ctessum/geom/encoding/shp"
    "github.com/fhs/go-netcdf/netcdf"
    "math"
    "encoding/csv"
)

const (
    pol         = "TotalPM25"
)

// OutputSpec defines what mortality outputs to generate
type OutputSpec struct {
    Mode   string   `json:"mode"`   // "allcause", "5cod", "individual", "multiple"
    Causes []string `json:"causes"` // List of causes for individual/multiple mode
    Ages   []string `json:"ages"`   // List of ages for individual/multiple mode
}

// Config holds all configuration parameters
type Config struct {
    DataDir     string     `json:"dataDir"`
    PopFile     string     `json:"popFile"`
    TotalPMFile string     `json:"totalPMFile"`
    GEMMFile    string     `json:"gemmFile"`
    ResultFile  string     `json:"resultFile"`
    OutputDir   string     `json:"outputDir"`
    OutputFile  string     `json:"outputFile"`
    ShpVarName  string     `json:"shpVarName"`
    NCVarName   string     `json:"ncVarName"`
    NCLayer     int        `json:"ncLayer"`
    OutputSpec  OutputSpec `json:"outputSpec"`
}

// Default configuration values
func defaultConfig() Config {
    return Config{
        DataDir:     "../dataDir/",
        PopFile:     "inputs/pop.shp",
        TotalPMFile: "inputs/totalpm.shp",
        GEMMFile:    "inputs/gemm_params.csv",
        ResultFile:  "/Users/sumilthakrar/UMN/Projects/GlobalAg/cropnh3/results/nh3manure/inmap_output.shp",
        OutputDir:   "output/",
        OutputFile:  "output.shp",
        ShpVarName:  "TotalPM25",
        NCVarName:   "IJ_AVG_S__NH4",
        NCLayer:     0,
        OutputSpec: OutputSpec{
            Mode:   "allcause",
            Causes: []string{},
            Ages:   []string{},
        },
    }
}

var (
    configFile = flag.String("config", "", "Path to JSON configuration file (optional)")
    resultFile = flag.String("resultFile", "", "Path to the PM2.5 result file (shapefile or NetCDF)")
    outputDir = flag.String("outputDir", "", "Directory to save output files")
    outputFile = flag.String("outputFile", "", "Name of the output shapefile")
    shpVarName = flag.String("shpVarName", "", "Shapefile variable/field name to read")
    ncVarName = flag.String("ncVarName", "", "NetCDF variable name to read")
    ncLayer = flag.Int("ncLayer", -1, "Vertical layer index to extract from NetCDF (0 = ground level)")
    dataDir = flag.String("dataDir", "", "Path to data directory containing inputs")
)

// loadConfig loads configuration from file and applies command-line overrides
func loadConfig() Config {
    flag.Parse()

    // Start with defaults
    config := defaultConfig()

    // Load from config file if provided
    if *configFile != "" {
        fmt.Printf("Loading configuration from %s\n", *configFile)
        data, err := ioutil.ReadFile(*configFile)
        check(err)
        err = json.Unmarshal(data, &config)
        check(err)
    }

    // Override with command-line flags (if provided)
    if *resultFile != "" {
        config.ResultFile = *resultFile
    }
    if *outputDir != "" {
        config.OutputDir = *outputDir
    }
    if *outputFile != "" {
        config.OutputFile = *outputFile
    }
    if *shpVarName != "" {
        config.ShpVarName = *shpVarName
    }
    if *ncVarName != "" {
        config.NCVarName = *ncVarName
    }
    if *ncLayer != -1 {
        config.NCLayer = *ncLayer
    }
    if *dataDir != "" {
        config.DataDir = *dataDir
    }

    return config
}

func main(){
    config := loadConfig()

    // Create output directory if it doesn't exist
    if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
        check(err)
    }

    fmt.Println("reading inputs")
// Getting file paths
    inmapCells, totpm           := getTots(filepath.Join(config.DataDir, config.TotalPMFile), "TotalPM25")

    // Determine if input is NetCDF or shapefile based on extension
    var oldCells []geom.Polygonal
    var resultpmgrid []float64

    if strings.HasSuffix(strings.ToLower(config.ResultFile), ".nc") {
        fmt.Println("Reading NetCDF input file...")
        oldCells, resultpmgrid = getNCData(config.ResultFile, config.NCVarName, config.NCLayer)
    } else {
        fmt.Println("Reading shapefile input...")
        oldCells, resultpmgrid = getTots(config.ResultFile, config.ShpVarName)
        // Normally it's this one, but I've changed it for ASEAN
//        oldCells, resultpmgrid = getShpData(config.ResultFile, config.ShpVarName)
    }
    resultpm, err               := regridMean(oldCells, inmapCells, resultpmgrid)
    check(err)
    _, population               := getShpData(filepath.Join(config.DataDir, config.PopFile), "TotalPop")

    // Process GEMM params
    f, err                      := os.Open(filepath.Join(config.DataDir, config.GEMMFile))
    check(err)
    defer f.Close()
    csvReader                   := csv.NewReader(f)
    gemmData, err               := csvReader.ReadAll()
    check(err)
    gemmAllVals                 := processGEMM(gemmData)

    // Generate outputs based on outputSpec mode
    switch config.OutputSpec.Mode {
    case "allcause":
        fmt.Println("Calculating all-cause mortality for adults 25+")
        attrib := getDeaths("all", "25", resultpm, totpm, population, gemmAllVals, config)
        writeTotDeaths(inmapCells, attrib, filepath.Join(config.OutputDir, config.OutputFile))

    case "5cod":
        fmt.Println("Calculating 5 causes of death (summed across all ages)")
        get5COD(gemmAllVals, inmapCells, resultpm, totpm, population, config)

    case "individual":
        if len(config.OutputSpec.Causes) != 1 || len(config.OutputSpec.Ages) != 1 {
            panic("individual mode requires exactly one cause and one age")
        }
        cause := config.OutputSpec.Causes[0]
        age := config.OutputSpec.Ages[0]
        fmt.Printf("Calculating mortality for cause=%s, age=%s\n", cause, age)
        attrib := getDeaths(cause, age, resultpm, totpm, population, gemmAllVals, config)
        writeTotDeaths(inmapCells, attrib, filepath.Join(config.OutputDir, config.OutputFile))

    case "multiple":
        if len(config.OutputSpec.Causes) == 0 || len(config.OutputSpec.Ages) == 0 {
            panic("multiple mode requires at least one cause and one age")
        }
        fmt.Printf("Calculating mortality for %d cause(s) x %d age(s) = %d outputs\n",
            len(config.OutputSpec.Causes), len(config.OutputSpec.Ages),
            len(config.OutputSpec.Causes)*len(config.OutputSpec.Ages))

        for _, cause := range config.OutputSpec.Causes {
            for _, age := range config.OutputSpec.Ages {
                fmt.Printf("  Processing: %s_%s\n", cause, age)
                attrib := getDeaths(cause, age, resultpm, totpm, population, gemmAllVals, config)
                outputName := fmt.Sprintf("%s_%s.shp", cause, age)
                writeTotDeaths(inmapCells, attrib, filepath.Join(config.OutputDir, outputName))
            }
        }

    default:
        panic(fmt.Sprintf("Unknown output mode: %s. Valid modes: allcause, 5cod, individual, multiple", config.OutputSpec.Mode))
    }
}

func get5COD (gemmAllVals []gemmAll, inmapCells []geom.Polygonal, resultpm, totpm, population []float64, config Config) {
    totAttrib := make([]float64, len(inmapCells))
    for _, c := range gemmAllVals {
//      Baseline mortality rates aren't saved out for IHD and STR for people aged 25+
//        if ((c.gk.cod == "all") || (c.gk.cod == "str") || (c.gk.cod == "ihd"))  && (c.gk.age == "25") {
//      Also, we do not want to sum allcause when calculating 5-COD.
        if (c.gk.cod == "all") {
//        if (c.gk.cod != "ihd") {
            continue
        }
        fmt.Println(c.gk.cod, c.gk.age)
        sl          := getDeaths(c.gk.cod, c.gk.age, resultpm, totpm, population, gemmAllVals, config)
        totAttrib   = sumSlices(sl,totAttrib)
    }
    fmt.Println("writing total deaths to file")
    writeTotDeaths(inmapCells, totAttrib, filepath.Join(config.OutputDir, config.OutputFile))
}

func sumSlices(x, y []float64) ([]float64) {
    z   := make([]float64, len(x))
    for i := 0; i < len(x); i++ {
        z[i] = x[i] + y[i]
    }
    return z
}

type gemmParams struct {
    θ   float64
    α   float64
    μ   float64
    v   float64
}

type gemmKey struct {
    cod string
    age string
}

type gemmAll struct {
    gp  gemmParams
    gk  gemmKey
    se  string
}

func processGEMM(data [][]string) []gemmAll {
    var gpAll []gemmAll
    var err error
    for i, line := range data {
        if i > 0 { // omit header line
            var rec gemmAll
            for j, field := range line {
                if j == 0 {
                    rec.gk.cod = field
                } else if j == 1 {
                    rec.gk.age = field
                } else if j == 2 {
                    rec.gp.θ, err  = strconv.ParseFloat(field,64)
                    check(err)
                } else if j == 3 {
                    rec.se = field
                } else if j == 4 {
                    rec.gp.α, err = strconv.ParseFloat(field,64)
                    check(err)
                } else if j == 5 {
                    rec.gp.μ, err = strconv.ParseFloat(field,64)
                    check(err)
                } else if j == 6 {
                    rec.gp.v, err = strconv.ParseFloat(field,64)
                    check(err)
                }
            }
            gpAll = append(gpAll, rec)
        }
    }
    return gpAll
}

func saveTotalDeaths(cause, age string, resultpm, totpm, population []float64, g []gemmAll, inmapCells []geom.Polygonal, config Config) {
    var demogFile, acmortFile, ijhatFile string
    var params gemmParams
    m := make(map[gemmKey]gemmParams)
    for _, line := range g {
        m[line.gk] = line.gp
    }
    params                  = m[gemmKey{cause,age}]
    demogFile               = filepath.Join(config.DataDir, "inputs","age"+age+".shp")
    acmortFile              = filepath.Join(config.DataDir, "basemorts",cause+age+".shp")
    ijhatFile               = filepath.Join(config.DataDir, "ijhats",cause+"_"+age+".shp")

    _, countryRegrid            := getTots(demogFile, "RRs")    // Change name
    _, allcausemort             := getTots(acmortFile, "RRs")   // Change name
    _, ijhat                    := getTots(ijhatFile, "RRs")    // Change name
    totdeaths                   := totDeaths(totpm, resultpm, population, ijhat, countryRegrid, allcausemort, params)
    writeTotDeaths(inmapCells, totdeaths, "deaths-totals.shp")
}

func getDeaths(cause, age string, resultpm, totpm, population []float64, g []gemmAll, config Config) []float64 {
    var demogFile, acmortFile, ijhatFile string
    var params gemmParams
    m := make(map[gemmKey]gemmParams)
    for _, line := range g {
        m[line.gk] = line.gp
    }
    params                  = m[gemmKey{cause,age}]
    demogFile               = filepath.Join(config.DataDir, "inputs","age"+age+".shp")
    acmortFile              = filepath.Join(config.DataDir, "basemorts",cause+age+".shp")
    ijhatFile               = filepath.Join(config.DataDir, "ijhats", cause+"_"+age+".shp")

    _, countryRegrid            := getTots(demogFile, "RRs")    // Change name
    _, allcausemort             := getTots(acmortFile, "RRs")   // Change name
    _, ijhat                    := getTots(ijhatFile, "RRs")    // Change name
    totdeaths                   := totDeaths(totpm, resultpm, population, ijhat, countryRegrid, allcausemort, params)
    attrib                      := attribution(totpm, totdeaths, resultpm)
    return attrib
}

func getNCData(ncFile, varName string, layer int) ([]geom.Polygonal, []float64) {
	ds, err := netcdf.OpenFile(ncFile, netcdf.NOWRITE)
	check(err)
	defer ds.Close()

	// Get dimensions
	latVar, err := ds.Var("lat")
	check(err)
	lats64, err := latVar.Len()
	check(err)
	lats := int(lats64)
	lat := make([]float32, lats)
	check(latVar.ReadFloat32s(lat))
	dy := lat[5] - lat[4] // Assume regular grid, first grid cell may be weird.

	lonVar, err := ds.Var("lon")
	check(err)
	lons64, err := lonVar.Len()
	check(err)
	lons := int(lons64)
	lon := make([]float32, lons)
	check(lonVar.ReadFloat32s(lon))
	dx := lon[5] - lon[4] // Assume regular grid, first grid cell may be weird.

	// Read the variable
	v, err := ds.Var(varName)
	check(err)

	// Check if variable is 2D or 3D by checking number of dimensions
	ndims, err := v.NAttrs()
	check(err)

	var ncData []float64

	// For 3D data (lev, lat, lon), extract single layer at ground level
	// We read a slice at the specified layer index
	if layer >= 0 {
		// 3D data - read single layer slice
		ncData = make([]float64, lats*lons)
		// ReadFloat64Slice expects (data, start indices, count)
		check(v.ReadFloat64Slice(ncData, []uint64{uint64(layer), 0, 0}, []uint64{1, uint64(lats), uint64(lons)}))
	} else {
		// This shouldn't happen with default layer=0, but kept for safety
		panic(fmt.Sprintf("Invalid layer index: %d", layer))
	}

	// Create grid cells
	gcCells := make([]geom.Polygonal, 0, len(ncData))
	for j := 0; j < lats; j++ {
		for i := 0; i < lons; i++ {
			gcCells = append(gcCells, &geom.Bounds{
				Min: geom.Point{X: float64(lon[i] - dx/2), Y: float64(lat[j] - dy/2)},
				Max: geom.Point{X: float64(lon[i] + dx/2), Y: float64(lat[j] + dy/2)},
			})
		}
	}

	gcVals := make([]float64, len(ncData))
	for i, v := range ncData {
		gcVals[i] = float64(v)
	}

	// Suppress unused variable warning
	_ = ndims

	return gcCells, gcVals
}


func totDeaths(totpm, resultpm, population, ijhat, countryRegrid, allcausemort []float64, params gemmParams) (deaths []float64) {
    var maxConc []float64
    for t := range totpm {
        var concs float64
        concs = math.Max(resultpm[t],totpm[t])
        maxConc = append(maxConc, concs)
        concs = totpm[t]
        dd := (GEMM(concs, params.θ, params.α, params.μ, params.v) - 1) * (population[t] / ijhat[t]) * countryRegrid[t] * allcausemort[t] / 100000
        deaths = append(deaths, dd)
    }
    return deaths
}

func GEMM(z, θ, α, μ, v float64) (float64) {
    z       =       math.Max(z-2.4,0)
    denom   :=      1.0 + math.Exp(-(z-μ)/v)
    numer   :=      θ * math.Log((z/α)+1)
    return math.Exp(numer/denom)
}

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

func regridMean(oldGeom, newGeom []geom.Polygonal, oldData []float64) (newData []float64, err error) {
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

// Handle errors
func check(err error) {
	if err != nil {
		panic(err)
	}
}

func writeTotDeaths(cells []geom.Polygonal, inputData []float64, filename string) {
	type shpOut struct {
		geom.Polygon
		TotalPopD float64
	}

	e, err := shp.NewEncoder(filename, shpOut{})
	check(err)
	for i, c := range cells {
		check(e.Encode(shpOut{
			Polygon:        c.Polygons()[0], // Assuming we are not using a multipolygon.
			TotalPopD:      inputData[i],
		}))
	}
	e.Close()
}

func attribution(totpm, totdeaths, resultpm []float64) ([]float64) {
    var attrib []float64
    for t := range totpm {
        var dd float64
        if totpm[t] == 0.0 {
            dd = 0.0
        } else {
            dd = resultpm[t] * totdeaths[t] / totpm[t]
            if math.IsNaN(dd) {
                dd = 0.0
            }
        }
        attrib = append(attrib, dd)
    }
    return attrib
}
