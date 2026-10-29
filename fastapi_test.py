from fastapi import FastAPI
import uvicorn

app = FastAPI()

@app.get("/")
async def root():
    return {"message": "FastAPI is working"}

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=5000)
