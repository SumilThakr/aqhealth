# aqhealth

Implementing concentration-response relationships to estimate the health effects of global changes in PM2.5 concentrations using the Global Exposure Mortality Model (GEMM).

## Overview

This tool calculates mortality attributable to PM2.5 air pollution by:
1. Reading PM2.5 concentration data from air quality models (shapefile or NetCDF format)
2. Spatially regridding concentrations to match population grids
3. Applying GEMM concentration-response functions
4. Calculating mortality estimates by cause and age group
5. Outputting results as shapefiles for GIS analysis

## Requirements

- Go 1.15 or higher (Go 1.23+ recommended)
- NetCDF C library (for NetCDF file support)
  - macOS: `brew install netcdf`
  - Linux: `sudo apt-get install libnetcdf-dev`

## Installation

### Building from Source

```bash
# Clone or navigate to the repository
cd aqhealth

# Build the executable
go build -o aqhealth main.go
```

This will create an `aqhealth` executable in the current directory.

## Usage

### Option 1: Configuration File (Recommended)

Create a `config.json` file (see `config.json` for a template with descriptions):

```json
{
  "dataDir": "../dataDir/",
  "popFile": "inputs/pop.shp",
  "totalPMFile": "inputs/totalpm.shp",
  "gemmFile": "inputs/gemm_params.csv",
  "resultFile": "/path/to/your/pm25_results.nc",
  "outputDir": "output/",
  "outputFile": "mortality_results.shp",
  "ncVarName": "IJ_AVG_S__NH4",
  "ncLayer": 0
}
```

Run with configuration file:

```bash
./aqhealth --config config.json
```

### Option 2: Command-Line Flags

```bash
./aqhealth --resultFile /path/to/input.nc \
           --outputDir results/ \
           --outputFile mortality.shp \
           --ncVarName IJ_AVG_S__NH4 \
           --ncLayer 0
```

### Option 3: Configuration File + Flag Overrides

Command-line flags override configuration file values:

```bash
./aqhealth --config config.json --resultFile different_input.nc --outputDir new_output/
```

## Configuration Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `dataDir` | Directory containing input data files | `../dataDir/` |
| `popFile` | Population shapefile (relative to dataDir) | `inputs/pop.shp` |
| `totalPMFile` | Baseline PM2.5 concentrations shapefile | `inputs/totalpm.shp` |
| `gemmFile` | GEMM parameters CSV file | `inputs/gemm_params.csv` |
| `resultFile` | PM2.5 result file (.shp or .nc) | Required |
| `outputDir` | Output directory (created if doesn't exist) | `output/` |
| `outputFile` | Output shapefile name | `output.shp` |
| `ncVarName` | NetCDF variable name (for .nc files) | `IJ_AVG_S__NH4` |
| `ncLayer` | Vertical layer to extract (0 = ground level) | `0` |

## Input File Formats

### Shapefile Input
Standard ESRI shapefile format with a `TotalPM25` attribute containing PM2.5 concentrations (μg/m³).

### NetCDF Input
GEOS-Chem or other model output in NetCDF format. The tool:
- Auto-detects NetCDF files by `.nc` extension
- Supports 2D (lat, lon) or 3D (lev, lat, lon) data
- Extracts ground-level concentrations (layer 0) by default
- Common variable names: `IJ_AVG_S__PM25`, `IJ_AVG_S__NH4`, etc.

## Output

The tool generates shapefiles containing:
- **TotalPopD**: Mortality estimates (deaths) per grid cell
- Output includes three files: `.shp`, `.dbf`, `.shx`

## Examples

### Example 1: Process GEOS-Chem NetCDF Output

```bash
./aqhealth --config config.json \
           --resultFile /data/geoschem/pm25_diff.nc \
           --ncVarName IJ_AVG_S__PM25
```

### Example 2: Process InMAP Shapefile Output

```bash
./aqhealth --resultFile /results/inmap_output.shp \
           --outputDir results/mortality/ \
           --outputFile inmap_mortality.shp
```

### Example 3: Different Vertical Layer

```bash
./aqhealth --config config.json \
           --resultFile model_output_3d.nc \
           --ncLayer 2
```

## Data Directory Structure

The `dataDir` should contain:

```
dataDir/
├── inputs/
│   ├── pop.shp           # Population data
│   ├── totalpm.shp       # Baseline PM2.5
│   ├── gemm_params.csv   # GEMM parameters
│   └── age25.shp         # Age-stratified population
├── basemorts/
│   └── all25.shp         # Baseline mortality rates
└── ijhats/
    └── all_25.shp        # Country adjustment factors
```

## Help

View all available flags:

```bash
./aqhealth -h
```
