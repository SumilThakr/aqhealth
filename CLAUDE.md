# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based tool for estimating health effects of global changes in PM2.5 concentrations using concentration-response relationships. It implements the Global Exposure Mortality Model (GEMM) to calculate mortality attributable to air pollution exposure.

## Build and Run Commands

```bash
# Build the executable
go build -o aqhealth main.go

# Run the program directly
go run main.go
```

## Architecture

### Core Workflow

The main execution flow (main.go:33-68):
1. Reads PM2.5 concentration data from shapefiles (total PM and result PM grids)
2. Loads population data and GEMM parameters from CSV
3. Regrids PM2.5 data to match population grid using spatial intersection
4. Calculates mortality using GEMM concentration-response functions
5. Outputs results as shapefiles

### Key Data Structures

- **gemmParams** (main.go:96-101): Contains GEMM model parameters (θ, α, μ, v)
- **gemmKey** (main.go:103-106): Identifies specific GEMM parameters by cause of death (cod) and age group
- **gemmAll** (main.go:108-112): Combines GEMM parameters with their keys and standard errors

### Critical Functions

- **GEMM()** (main.go:239-244): Implements the core concentration-response relationship. Uses log-linear function with logistic transition at concentration threshold (z-2.4 μg/m³).
- **regridMean()** (main.go:290-319): Spatially regrids data between different grid resolutions using area-weighted mean values. Uses R-tree spatial index for efficient intersection queries.
- **getDeaths()** (main.go:166-184): Calculates mortality for a specific cause and age group by combining PM concentrations, population, baseline mortality rates, and GEMM parameters.
- **attribution()** (main.go:345-360): Proportionally attributes total deaths to specific PM2.5 sources.

### Data Dependencies

The program expects the following directory structure relative to `../dataDir/`:
- `inputs/pop.shp`: Population data
- `inputs/totalpm.shp`: Total PM2.5 concentrations
- `inputs/gemm_params.csv`: GEMM model parameters
- `inputs/age{XX}.shp`: Age-stratified population fractions
- `basemorts/{cause}{age}.shp`: Baseline mortality rates by cause and age
- `ijhats/{cause}_{age}.shp`: Country-specific adjustment factors

Output is written to `output.shp` by default.

## Configuration

Key constants are hardcoded in main.go:17-31. The most commonly modified are:
- `dataDir`: Path to input data directory
- `resultFile`: Path to the PM2.5 result file to analyze
- `outputFile`: Name of output shapefile
- NetCDF settings (`ncFile`, `varName`, `lats`, `lons`) for alternative input format

## Dependencies

- `github.com/ctessum/geom`: Spatial geometry operations and R-tree spatial indexing
- `github.com/fhs/go-netcdf`: NetCDF file reading (for alternative input format)

## Analysis Modes

The code supports multiple analysis modes (see main.go:54-67):
- All-cause mortality (`getDeaths("all", "25", ...)`)
- Five causes of death separately (`get5COD(...)` - calculates IHD, stroke, lung cancer, COPD, LRI)
- Individual cause-specific mortality (commented examples for lcancer, copd, lri)
