package main

import (
    "os"
    "fmt"
	"strconv"
    "path/filepath"
    "strings"
    "github.com/ctessum/geom/index/rtree"
	"github.com/ctessum/geom"
	"github.com/ctessum/geom/encoding/shp"
    "github.com/fhs/go-netcdf/netcdf"
    "math"
    "encoding/csv"
)

const (
    dataDir     = "../../mortality"
    popF     = "inputs/pop.shp"
    totalPM      = "inputs/totalpm.shp"
    pol         = "TotalPM25"
    resultFile = "/Users/sumilthakrar/UMN/Projects/GlobalAg/cropnh3/results/nh3manure/inmap_output.shp"
    gemmFile    = "inputs/gemm_params.csv"
    outputFile = "output.shp"
    // if reading NetCDF inputs:
	ncFile  =  "/Users/sumilthakrar/UMN/Projects/GlobalAg/livestock/GEOS-Chem/difference.nc"
	layer   = 0
	varName = "PM25"
	lats    = 91
	lons    = 144
)

func main(){
    fmt.Println("reading inputs")
// Getting file paths
    inmapCells, totpm           := getTots(filepath.Join(dataDir, totalPM), "TotalPM25")
    oldCells, resultpmgrid      := getTots(resultFile, "TotalPM25")
    // Normally it's this one, but I've changed it for ASEAN
//    oldCells, resultpmgrid      := getShpData(resultFile, "TotalPM25")
    resultpm, err               := regridMean(oldCells, inmapCells, resultpmgrid)
    check(err)
    _, population               := getShpData(filepath.Join(dataDir, popF), "TotalPop")

    // Process GEMM params
    f, err                      := os.Open(filepath.Join(dataDir, gemmFile))
    check(err)
    defer f.Close()
    csvReader                   := csv.NewReader(f)
    gemmData, err               := csvReader.ReadAll()
    check(err)
    gemmAllVals                 := processGEMM(gemmData)

    // For testing:
//    saveTotalDeaths("all", "25", resultpm, totpm, population, gemmAllVals, inmapCells)
//    get5COD(gemmAllVals, inmapCells, totpm, totpm, population)
    // 5COD
//    get5COD(gemmAllVals, inmapCells, resultpm, totpm, population)
    // All natural cause:
    attrib                      := getDeaths("all", "25", resultpm, totpm, population, gemmAllVals)
    writeTotDeaths(inmapCells, attrib, outputFile)
    // Individual results:
//    attrib                    := getDeaths("lcancer", "25", resultpm, totpm, population, gemmAllVals)
//    writeTotDeaths(inmapCells, attrib, "deaths-lcancer.shp")
//    attrib                    = getDeaths("copd", "25", resultpm, totpm, population, gemmAllVals)
//    writeTotDeaths(inmapCells, attrib, "deaths-copd.shp")
//    attrib                    = getDeaths("lri", "25", resultpm, totpm, population, gemmAllVals)
//    writeTotDeaths(inmapCells, attrib, "deaths-lri.shp")
}

func get5COD (gemmAllVals []gemmAll, inmapCells []geom.Polygonal, resultpm, totpm, population []float64) {
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
        sl          := getDeaths(c.gk.cod, c.gk.age, resultpm, totpm, population, gemmAllVals)
        totAttrib   = sumSlices(sl,totAttrib)
    }
    fmt.Println("writing total deaths to file")
    writeTotDeaths(inmapCells, totAttrib, outputFile)
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

func saveTotalDeaths(cause, age string, resultpm, totpm, population []float64, g []gemmAll, inmapCells []geom.Polygonal) {
    var demogFile, acmortFile, ijhatFile string
    var params gemmParams
    m := make(map[gemmKey]gemmParams)
    for _, line := range g {
        m[line.gk] = line.gp
    }
    params                  = m[gemmKey{cause,age}]
    demogFile               = filepath.Join(dataDir, "inputs","age"+age+".shp")
    acmortFile              = filepath.Join(dataDir, "basemorts",cause+age+".shp")
    ijhatFile               = filepath.Join(dataDir, "ijhats",cause+"_"+age+".shp")

    _, countryRegrid            := getTots(demogFile, "RRs")    // Change name
    _, allcausemort             := getTots(acmortFile, "RRs")   // Change name
    _, ijhat                    := getTots(ijhatFile, "RRs")    // Change name
    totdeaths                   := totDeaths(totpm, resultpm, population, ijhat, countryRegrid, allcausemort, params)
    writeTotDeaths(inmapCells, totdeaths, "deaths-totals.shp")
}

func getDeaths(cause, age string, resultpm, totpm, population []float64, g []gemmAll) []float64 {
    var demogFile, acmortFile, ijhatFile string
    var params gemmParams
    m := make(map[gemmKey]gemmParams)
    for _, line := range g {
        m[line.gk] = line.gp
    }
    params                  = m[gemmKey{cause,age}]
    demogFile               = filepath.Join(dataDir, "inputs","age"+age+".shp")
    acmortFile              = filepath.Join(dataDir, "basemorts",cause+age+".shp")
    ijhatFile               = filepath.Join(dataDir, "ijhats", cause+"_"+age+".shp")

    _, countryRegrid            := getTots(demogFile, "RRs")    // Change name
    _, allcausemort             := getTots(acmortFile, "RRs")   // Change name
    _, ijhat                    := getTots(ijhatFile, "RRs")    // Change name
    totdeaths                   := totDeaths(totpm, resultpm, population, ijhat, countryRegrid, allcausemort, params)
    attrib                      := attribution(totpm, totdeaths, resultpm)
    return attrib
}

func getNCData() ([]geom.Polygonal, []float64) {
	ds, err := netcdf.OpenFile(ncFile, netcdf.NOWRITE)
	check(err)

	latVar, err := ds.Var("lat")
	check(err)
	lat := make([]float32, lats)
	check(latVar.ReadFloat32s(lat))
	dy := lat[5] - lat[4] // Assume regular grid, first grid cell may be weird.
//	fmt.Println("dy =", dy)

	lonVar, err := ds.Var("lon")
	check(err)
	lon := make([]float32, lons)
	check(lonVar.ReadFloat32s(lon))
	dx := lon[5] - lon[4] // Assume regular grid, first grid cell may be weird.
//	fmt.Println("dx =", dx)

	v, err := ds.Var(varName)
	check(err)
	ncData := make([]float64, lats*lons)
	check(v.ReadFloat64Slice(ncData, []uint64{0, 0}, []uint64{lats, lons}))

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
