import {HttpClient, HttpParams} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  DeploymentTimeline,
  DeploymentTimelineComparison,
  DeploymentTimelineQuery,
  DeploymentTimelineRedeploy,
} from '../types/deployment-timeline';

const baseUrl = '/api/v1/deployment-timeline';

@Injectable({
  providedIn: 'root',
})
export class DeploymentTimelineService {
  private readonly httpClient = inject(HttpClient);

  list(query: DeploymentTimelineQuery = {}): Observable<DeploymentTimeline> {
    return this.httpClient.get<DeploymentTimeline>(baseUrl, {params: this.queryParams(query)});
  }

  compare(baseTaskId: string, compareTaskId: string): Observable<DeploymentTimelineComparison> {
    return this.httpClient.get<DeploymentTimelineComparison>(`${baseUrl}/compare`, {
      params: new HttpParams().set('baseTaskId', baseTaskId).set('compareTaskId', compareTaskId),
    });
  }

  redeploy(taskId: string): Observable<DeploymentTimelineRedeploy> {
    return this.httpClient.post<DeploymentTimelineRedeploy>(`${baseUrl}/${taskId}/redeploy`, null);
  }

  private queryParams(query: DeploymentTimelineQuery): HttpParams {
    let params = new HttpParams();
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== null && value !== '') {
        params = params.set(key, String(value));
      }
    }
    return params;
  }
}
